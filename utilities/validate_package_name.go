package utilities

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode"
)

var match = regexp.MustCompile(`^(?:@([^/]+?)[/])?([^/]+?)$`)

const maxLen = 214

// Validate : abstracted over from npm validate-npm-package-name
func Validate(name string) error {
	blockList := map[string]bool{
		"node_modules": true,
		"favicon.ico":  true,
	}

	if len(name) == 0 {
		return errors.New("no package name provided")
	}

	if name[0] == '_' {
		return fmt.Errorf("package name cannot start with '_': %s", name)
	}

	if name[0] == '.' {
		return fmt.Errorf("package name cannot start with '.': %s", name)
	}

	if containsAnySpace(name) != -1 {
		return fmt.Errorf("package name must not have any whitespace: %s", name)
	}

	if _, ok := blockList[strings.ToLower(name)]; ok {
		return fmt.Errorf("package name must not be any blocklist items: %s", name)
	}

	if len(name) > maxLen {
		return fmt.Errorf("package name must not exceed 214 characters: %v", len(name))
	}

	if strings.ToLower(name) != name {
		return fmt.Errorf("package name must not contain any upper case letters: %s", name)
	}

	if containsBlockRunes(name) {
		return fmt.Errorf("package name must not contain any blocked runes: %s", name)
	}

	if !match.MatchString(name) {
		return fmt.Errorf("package name must contain only URL-friendly characters: %s", name)
	}

	return nil
}

func containsAnySpace(s string) int {
	for i, r := range s {
		if unicode.IsSpace(r) {
			return i
		}
	}
	return -1
}

func containsBlockRunes(name string) bool {
	pattern := []rune(`~'!()*"`)
	runes := []rune(name)
	// O(N*P)

	for _, c := range runes {
		for _, p := range pattern {
			if p == c {
				log.Printf("?: %s", string(rune(p)))
				return true
			}
		}
	}
	return false
}
