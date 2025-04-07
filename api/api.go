package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/gorilla/mux"
	"github.com/snyk/snyk-code-review-exercise/utilities"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
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
	pkgName := vars["package"]
	pkgVersion := vars["version"] // validated by semver package

	if err := utilities.Validate(pkgName); err != nil {
		log.Printf("Error validating package name: %v", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	log.Printf("Package: %s, Version: %s", pkgName, pkgVersion)

	rootPkg := &NpmPackageVersion{Name: pkgName, Dependencies: map[string]*NpmPackageVersion{}}
	if err := resolveDependencies(rootPkg, pkgVersion); err != nil {
		log.Printf("Error resolving dependencies: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	output, err := json.MarshalIndent(rootPkg, "", "  ")
	if err != nil {
		log.Printf("Error marshalling dependency tree: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// idea: consider adding gzip, we may be sending a lot of information and this would save on bandwidth and latency
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	n, err := w.Write(output)
	if err != nil {
		log.Printf("Error writing response body (%d bytes written): %v", n, err)
	}
}

// review: inconsistent variable nomenclature, consider using `ver` or `version` instead of `versionConstraint` as it's overly specific
// idea: recursion can cause stack overflows, consider a linear approach
// review: this is slow, consider using a worker pool & mutex/atomics
func resolveDependencies(pkg *NpmPackageVersion, version string) error {
	pkgMeta, err := fetchPackageMeta(pkg.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata for <%s,%s>: %v", pkg.Name, pkg.Version, err)
	}

	if pkgMeta == nil {
		return fmt.Errorf("failed to fetch package metadata (nil pointer) for <%s,%s>", pkg.Name, pkg.Version)
	}

	pkg.Version, err = highestCompatibleVersion(version, pkgMeta)
	if err != nil {
		return fmt.Errorf("failed to resolve compatible existing version for <%s,%s>: %v", pkg.Name, pkg.Version, err)
	}

	// review: because of `npmPkg`, `pkg.Dependencies` is never used, so it's a wasted variable
	npmPkg, err := fetchPackage(pkg.Name, pkg.Version)
	if err != nil {
		return fmt.Errorf("failed to fetch package dependencies for <%s,%s>: %v", pkg.Name, pkg.Version, err)
	}

	// review: our code does nothing to prevent circular dependencies
	// review: simplify variable names `depName, depVer` Go style guides support this as this will be clear enough
	for name, v := range npmPkg.Dependencies {
		dep := &NpmPackageVersion{Name: name, Dependencies: map[string]*NpmPackageVersion{}}
		pkg.Dependencies[name] = dep
		if err = resolveDependencies(dep, v); err != nil {
			return fmt.Errorf("failed to resolve dependencies for <%s,%s>: %v", pkg.Name, pkg.Version, err)
		}
	}
	return nil
}

func highestCompatibleVersion(version string, pkgMeta *npmPackageMetaResponse) (string, error) {
	constraint, err := semver.NewConstraint(normalizeConstraint(version))
	if err != nil {
		return "", err
	}
	filtered := filterCompatibleVersions(constraint, pkgMeta)
	sort.Sort(filtered)
	if len(filtered) == 0 {
		return "", errors.New("no compatible version found")
	}
	return filtered[len(filtered)-1].String(), nil
}

func filterCompatibleVersions(constraint *semver.Constraints, pkgMeta *npmPackageMetaResponse) semver.Collection {
	var compatible semver.Collection
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

func normalizeConstraint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "*" {
		return ">= 0.0.0"
	}
	if raw == "" {
		return ">= 0.0.0"
	}
	if matched, _ := regexp.MatchString(`^\d+$`, raw); matched {
		return raw + ".x.x"
	}
	if matched, _ := regexp.MatchString(`^\d+\.\d+$`, raw); matched {
		return raw + ".x"
	}
	return raw
}

func fetchPackage(name, version string) (*npmPackageResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s/%s", name, version))
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		errDef := Body.Close()
		if errDef != nil {
			log.Printf("Error closing body while fetching package: %v", err)
			return
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageResponse

	err = json.Unmarshal(body, &parsed)
	if err != nil {
		return nil, fmt.Errorf("error during package unmarshal: %v", err)
	}

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
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://registry.npmjs.org/%s", p), nil)
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	resp, err := http.DefaultClient.Do(req)

	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch package metadata for <%s>: %v", p, err)
	}
	// review: Add error handling to the deferred close
	defer func(Body io.ReadCloser) {
		errDef := Body.Close()
		if errDef != nil {
			log.Printf("Error closing body wihle fetching metadata: %v", err)
			return
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageMetaResponse
	if err = json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("error during metadata unmarshal: %v", err)
	}

	return &parsed, nil
}
