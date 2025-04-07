package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/gorilla/mux"
)

func New() http.Handler {
	router := mux.NewRouter()
	// idea: add API versioning to allow safe iteration on breaking changes (e.g. /v1/package/...)
	router.Handle("/package/{package}/{version}", http.HandlerFunc(packageHandler))
	return router
}

type npmPackageMetaResponse struct {
	Versions map[string]npmPackageResponse `json:"versions"`
}

type npmPackageResponse struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// NpmPackageVersion review: this is capitalised, and therefore exported, does it need to be?
// NpmPackageVersion review: this is almost a copy of npmPackageResponse.
type NpmPackageVersion struct {
	Name         string                        `json:"name"`
	Version      string                        `json:"version"`
	Dependencies map[string]*NpmPackageVersion `json:"dependencies"`
}

func packageHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// review: we should validate and sanitise our inputs, especially if they will be sent to other services in the future
	pkgName := vars["package"]
	pkgVersion := vars["version"]

	rootPkg := &NpmPackageVersion{Name: pkgName, Dependencies: map[string]*NpmPackageVersion{}}
	if err := resolveDependencies(rootPkg, pkgVersion); err != nil {
		// review: use logging - timestamps, log levels, stderr, formatting, extensible - best for observability
		println(err.Error())
		// review: magic number - http.StatusInternalServerError
		w.WriteHeader(500)
		return
	}

	// review: consider more appropriate name, like `res` or `out`
	// review: use json.Marshal(rootPkg) for prod - reduces payload size - keep Indent for dev maybe
	// review 2: However, this can stay as its AC #2.
	stringified, err := json.MarshalIndent(rootPkg, "", "  ")
	if err != nil {
		// review: use logging - timestamps, log levels, stderr, formatting, extensible - best for observability
		println(err.Error())
		// review: magic number - http.StatusInternalServerError
		w.WriteHeader(500)
		return
	}

	// idea: consider adding gzip, we may be sending a lot of information and this would save on bandwidth and latency
	w.Header().Set("Content-Type", "application/json")
	// review: magic number - http.StatusOk
	w.WriteHeader(200)

	// review: ignoring errors from `w.Write` could hide failed responses like when a client disconnects
	// Ignoring ResponseWriter errors
	_, _ = w.Write(stringified)

	// idea: add logging for successful result
}

// review: inconsistent variable nomenclature, consider using `ver` or `version` instead of `versionConstraint` as it's overly specific
// idea: recursion can cause stack overflows, consider a linear approach
// review: this is slow, consider using a worker pool & mutex/atomics
func resolveDependencies(pkg *NpmPackageVersion, versionConstraint string) error {
	pkgMeta, err := fetchPackageMeta(pkg.Name)
	if err != nil {
		// review: Add logging & wrapping: 'failed to fetch package metadata for <pkg.Name>'
		return err
	}
	concreteVersion, err := highestCompatibleVersion(versionConstraint, pkgMeta)
	if err != nil {
		// review: Add logging & wrapping: 'failed to resolve version for <pkg.Name> with constraint <versionConstraint>'
		return err
	}
	// review: wasted variable, just initially assign pkg.Version
	pkg.Version = concreteVersion

	// review: because of `npmPkg`, `pkg.Dependencies` is never used, so it's a wasted variable
	npmPkg, err := fetchPackage(pkg.Name, pkg.Version)
	if err != nil {
		// review: Add logging & wrapping: 'failed to fetch package dependencies <pkg.Version> <pkg.Name>'
		// this will help especially with deep recursion
		return err
	}
	// review: our code does nothing to prevent circular dependencies
	// review: simplify variable names `depName, depVer` Go style guides support this as this will be clear enough
	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		dep := &NpmPackageVersion{Name: dependencyName, Dependencies: map[string]*NpmPackageVersion{}}
		pkg.Dependencies[dependencyName] = dep
		// review: shadowed err variable, use another name like `err2` to improve readability and prevent bugs
		if err := resolveDependencies(dep, dependencyVersionConstraint); err != nil {
			// review: Add logging & wrapping: 'failed to resolve dependencies for <pkg.Version> <pkg.Name>'
			// this will help especially with deep recursion
			return err
		}
	}
	return nil
}

// review: `versions` is Package Metadata, and has no conflicting names within this scope, this inconsistency increases reading time
// review: rename both parameters `version string, pkgMeta *npm...`
func highestCompatibleVersion(constraintStr string, versions *npmPackageMetaResponse) (string, error) {
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return "", err
	}
	filtered := filterCompatibleVersions(constraint, versions)
	sort.Sort(filtered)
	if len(filtered) == 0 {
		return "", errors.New("no compatible versions found")
	}
	return filtered[len(filtered)-1].String(), nil
}

func filterCompatibleVersions(constraint *semver.Constraints, pkgMeta *npmPackageMetaResponse) semver.Collection {
	var compatible semver.Collection
	// review: between fetchPackageMeta and here, there are no nil pointer checks on pkgMeta
	for version := range pkgMeta.Versions {
		semVer, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		if constraint.Check(semVer) {
			compatible = append(compatible, semVer)
		}
	}
	return compatible
}

func fetchPackage(name, version string) (*npmPackageResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s/%s", name, version))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageResponse
	// review: ignored error could cause undocumented bugs
	_ = json.Unmarshal(body, &parsed)
	return &parsed, nil
}

func fetchPackageMeta(p string) (*npmPackageMetaResponse, error) {

	// review: This endpoint returns the full package metadata (5MB+ for some packages, like `npm`),
	// which causes high latency and unnecessary memory usage.
	// You can request the abbreviated format with:
	//   Accept: application/vnd.npm.install-v1+json
	// More: https://github.com/npm/registry/blob/main/docs/responses/package-metadata.md#package-metadata
	// This means we will need to switch from http.Get to http.NewRequest.

	// review 2: http.Get does not return an error on 400 or 500 status codes, check resp.Status manually

	// review 3: so, the most optimal thing to do is, fetch the abbreviated metadata,
	// then use that to find the latest semantic version, which has the dependencies included already,
	// -> no need for a double fetch per package
	url := fmt.Sprintf("https://registry.npmjs.org/%s", p)
	resp, err := http.Get(url)
	log.Printf("url: %s", url)
	log.Printf("resp: %s", resp)
	// review: `err != nil || resp.Status != http.StatusOK`
	if err != nil {
		log.Printf("err: %s", err)
		// review: Add logging & wrapping: 'failed to resolve version for <pkg.Name> with constraint <versionConstraint>'
		return nil, err
	}
	// review: Add error handling to the deferred close
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageMetaResponse
	// review: type case `[]byte()` is redundant, `body` is already a Byte array
	// review 2: shadowed error, rename to `err2` or anything else
	// idea: consider using streaming decoder `json.NewDecoder(resp.Body)` for lower memory usage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}
