package datasource

import (
	"path/filepath"
	"testing"
)

const proxiedMetadata = `{"backend":"dolt","dolt_mode":"proxied-server","dolt_database":"hq"}`

// A Gas City store: bd supervises a loopback proxy in front of a private
// dolt sql-server and records the proxy's port in proxy.pid.
func TestDiscoverDoltSources_ProxiedServerDialsTheProxy(t *testing.T) {
	isolateDoltAddressEnv(t)
	t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	writeFileAll(t, filepath.Join(beadsDir, "metadata.json"), proxiedMetadata)
	writeFileAll(t, filepath.Join(beadsDir, "dolt", "proxy.pid"), `{"pid":61957,"port":51869,"schema":2,"kind":"db-proxy","control_port":51870}`)
	writeFileAll(t, filepath.Join(beadsDir, "dolt", "config.yaml"), "listener:\n    host: 127.0.0.1\n    port: 57549\n")

	sources, err := discoverDoltSources(beadsDir, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %+v, want one", sources)
	}
	got := sources[0]
	if got.Type != SourceTypeDolt || got.Path != "127.0.0.1:51869" || got.Database != "hq" || got.User != "root" || !got.Passwordless {
		t.Fatalf("source = %+v, want passwordless root Dolt source hq at 127.0.0.1:51869", got)
	}
}

// Without a proxy record, the backend listener is the only address left.
func TestDiscoverDoltSources_ProxiedServerFallsBackToBackendListener(t *testing.T) {
	isolateDoltAddressEnv(t)
	t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	writeFileAll(t, filepath.Join(beadsDir, "metadata.json"), proxiedMetadata)
	writeFileAll(t, filepath.Join(beadsDir, "dolt", "config.yaml"), "listener:\n    port: 57549\n")

	sources, err := discoverDoltSources(beadsDir, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Path != "127.0.0.1:57549" {
		t.Fatalf("sources = %+v, want one source at 127.0.0.1:57549", sources)
	}
}

// The client-info sidecar may move the proxy root out of .beads/dolt.
func TestDiscoverDoltSources_ProxiedServerHonoursSidecarRootPath(t *testing.T) {
	isolateDoltAddressEnv(t)
	t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	writeFileAll(t, filepath.Join(beadsDir, "metadata.json"), proxiedMetadata)
	writeFileAll(t, filepath.Join(beadsDir, "proxied_server_client_info.json"), `{"root_path":"elsewhere","idle_timeout":-1}`)
	writeFileAll(t, filepath.Join(beadsDir, "elsewhere", "proxy.pid"), `{"pid":1,"port":40001,"kind":"db-proxy"}`)

	sources, err := discoverDoltSources(beadsDir, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Path != "127.0.0.1:40001" {
		t.Fatalf("sources = %+v, want one source at 127.0.0.1:40001", sources)
	}
}

// The proxy authenticates root without a password. Sending the shared-server
// password there is refused by Dolt, and leaks a secret to a server that
// never asked for it.
func TestDoltConnectionConfig_PasswordlessSourceSendsNoPassword(t *testing.T) {
	t.Setenv("BEADS_DOLT_PASSWORD", "shared-secret")

	cfg, err := doltConnectionConfig(DataSource{Type: SourceTypeDolt, Path: "127.0.0.1:51869", User: "root", Passwordless: true}, "hq")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Passwd != "" {
		t.Fatalf("Passwd = %q, want empty", cfg.Passwd)
	}
}

// The combined view reads every checkout's database; a Gas City checkout
// there must keep its passwordless proxy credential.
func TestDiscoverDoltDBs_KeepsProxiedServerPasswordless(t *testing.T) {
	isolateDoltAddressEnv(t)
	t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
	project := t.TempDir()
	beadsDir := filepath.Join(project, ".beads")
	writeFileAll(t, filepath.Join(beadsDir, "metadata.json"), proxiedMetadata)
	writeFileAll(t, filepath.Join(beadsDir, "dolt", "proxy.pid"), `{"pid":1,"port":51869,"kind":"db-proxy"}`)

	dbs := DiscoverDoltDBs(map[string]string{"city": project})

	if len(dbs) != 1 || !dbs[0].Passwordless || dbs[0].Host != "127.0.0.1:51869" {
		t.Fatalf("DiscoverDoltDBs = %+v, want one passwordless database at 127.0.0.1:51869", dbs)
	}
}
