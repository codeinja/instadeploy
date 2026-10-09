package main

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	// Lowercase DNS label, so it can be part of a public hostname.
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

	// Compose service names (also used in hostnames, so keep them simple).
	serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

	// Docker image reference: [registry[:port]/]path[:tag][@sha256:digest]
	imageRe = regexp.MustCompile(`^` +
		`(?:[a-zA-Z0-9]+(?:[.-][a-zA-Z0-9]+)*(?::[0-9]+)?/)?` + // optional registry host
		`[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*` + // first path component
		`(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*` + // more path components
		`(?::[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127})?` + // tag
		`(?:@sha256:[a-f0-9]{64})?$`) // digest

	envKeyRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	volumeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	gitRefRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	hostnameRe   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	uuidRe       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func validImage(image string) bool { return len(image) <= 255 && imageRe.MatchString(image) }
func validPort(p int) bool         { return p >= 1 && p <= 65535 }
func validUUID(s string) bool      { return uuidRe.MatchString(s) }

func validateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name must be 1-32 lowercase letters, numbers or dashes (for example my-app)")
	}
	return nil
}

func validateEnvKey(key string) error {
	if !envKeyRe.MatchString(key) || len(key) > 128 {
		return fmt.Errorf("%q is not a valid variable name (use letters, numbers and _, not starting with a number)", key)
	}
	return nil
}

// validateGitURL only allows https:// repositories. That rules out
// ssh://, file:// and "--option" style values that could change what git does.
func validateGitURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		strings.HasPrefix(raw, "-") || len(raw) > 500 {
		return fmt.Errorf("enter an https:// Git repository URL, like https://github.com/user/repo")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return fmt.Errorf("Git repositories on private addresses aren't supported")
	}
	return nil
}

func validateHostname(h string) error {
	if len(h) > 253 || !hostnameRe.MatchString(h) {
		return fmt.Errorf("%q is not a valid domain name (for example app.example.com)", h)
	}
	return nil
}
