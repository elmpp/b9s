package datasource

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// A proxied-server store (dolt_mode "proxied-server", the mode Gas City
// workspaces use) has no fixed server address. bd supervises a loopback proxy
// in front of a private dolt sql-server and records the proxy's port in
// proxy.pid under the proxy root. bd itself connects through that proxy as
// root with no password, so b9s does the same.
const (
	proxiedServerMode           = "proxied-server"
	proxiedServerClientInfoFile = "proxied_server_client_info.json"
	proxiedServerRootPathEnv    = "BEADS_PROXIED_SERVER_ROOT_PATH"
	proxyPIDFileName            = "proxy.pid"
	proxiedServerUser           = "root"
)

// proxiedServerSource describes the proxy bd would dial for beadsDir. When no
// proxy is recorded, the backend's listener port is the best remaining guess;
// a dial there fails with a clear error rather than reading the wrong store.
func proxiedServerSource(beadsDir string, meta beadsMetadata, opts DiscoveryOptions) DataSource {
	root := proxiedServerRoot(beadsDir)
	port := proxyPort(filepath.Join(root, proxyPIDFileName))
	if port == 0 {
		port = listenerPort(filepath.Join(root, "config.yaml"))
	}
	if port == 0 {
		port = defaultLocalDoltPort
	}
	database := meta.DoltDatabase
	if database == "" {
		database = "beads"
	}
	modTime := time.Now()
	if info, err := os.Stat(root); err == nil {
		modTime = info.ModTime()
	}
	source := DataSource{
		Type:         SourceTypeDolt,
		Path:         net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Priority:     PriorityDolt,
		ModTime:      modTime,
		Database:     database,
		User:         proxiedServerUser,
		Passwordless: true,
	}
	if opts.Verbose {
		opts.Logger(fmt.Sprintf("Found proxied Dolt: %s/%s (root=%s)", source.Path, database, root))
	}
	return source
}

// proxiedServerRoot follows bd's order: the environment, then the root_path
// in the client-info sidecar (relative to beadsDir), then the Dolt data dir.
func proxiedServerRoot(beadsDir string) string {
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(beadsDir, p)
	}
	if p := os.Getenv(proxiedServerRootPathEnv); p != "" {
		return resolve(p)
	}
	if data, err := os.ReadFile(filepath.Join(beadsDir, proxiedServerClientInfoFile)); err == nil {
		var info struct {
			RootPath string `json:"root_path"`
		}
		if json.Unmarshal(data, &info) == nil && info.RootPath != "" {
			return resolve(info.RootPath)
		}
	}
	return doltDataDir(beadsDir)
}

func proxyPort(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var record struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(data, &record) != nil || record.Port <= 0 {
		return 0
	}
	return record.Port
}
