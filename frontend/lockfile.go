package frontend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// publicNpmRegistryHost is the only resolved-tarball host a committed
// lockfile may use. npm 12's allow-remote=none treats a tarball whose
// origin is not the install registry as a remote package and refuses
// it (EALLOWREMOTE). A lockfile written against a mirror therefore
// breaks `go run` on a machine that uses the default registry.
const publicNpmRegistryHost = "registry.npmjs.org"

func checkLockfileRegistry(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "package-lock.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	hosts, err := lockfileForeignHosts(data)
	if err != nil {
		return fmt.Errorf("package-lock.json: %w", err)
	}
	if len(hosts) == 0 {
		return nil
	}
	return fmt.Errorf("package-lock.json resolves against %s; npm 12 treats those tarballs as remote packages (EALLOWREMOTE) when the install registry is https://%s/. Rewrite the resolved URLs to that host", strings.Join(hosts, ", "), publicNpmRegistryHost)
}

func lockfileForeignHosts(data []byte) ([]string, error) {
	var lf struct {
		Packages map[string]struct {
			Resolved string `json:"resolved"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var hosts []string
	for _, pkg := range lf.Packages {
		if pkg.Resolved == "" {
			continue
		}
		u, err := url.Parse(pkg.Resolved)
		if err != nil {
			return nil, fmt.Errorf("resolved %q: %w", pkg.Resolved, err)
		}
		host := u.Host
		if host == "" {
			host = pkg.Resolved
		}
		if host == publicNpmRegistryHost {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts, nil
}
