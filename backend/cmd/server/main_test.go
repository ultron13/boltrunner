package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvOrReturnsEnvValueWhenSet(t *testing.T) {
	t.Setenv("BOLTRUNNER_TEST_ENVOR_KEY", "from-env")
	if got := envOr("BOLTRUNNER_TEST_ENVOR_KEY", "fallback"); got != "from-env" {
		t.Fatalf("expected %q, got %q", "from-env", got)
	}
}

func TestEnvOrReturnsFallbackWhenUnset(t *testing.T) {
	os.Unsetenv("BOLTRUNNER_TEST_ENVOR_UNSET_KEY")
	if got := envOr("BOLTRUNNER_TEST_ENVOR_UNSET_KEY", "fallback"); got != "fallback" {
		t.Fatalf("expected %q, got %q", "fallback", got)
	}
}

const minimalValidKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`

// buildK8sClient always tries in-cluster config first; outside a real
// cluster (as in this test binary) that reliably fails, so every case here
// exercises the kubeconfig fallback path.

func TestBuildK8sClientMissingKubeconfigErrors(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	if _, err := buildK8sClient(); err == nil {
		t.Fatal("expected an error when KUBECONFIG points at a nonexistent file")
	}
}

func TestBuildK8sClientValidKubeconfigSucceeds(t *testing.T) {
	kubeconfigPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfigPath, []byte(minimalValidKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	client, err := buildK8sClient()
	if err != nil {
		t.Fatalf("buildK8sClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected a non-nil client")
	}
}

func TestBuildK8sClientFallsBackToHomeKubeDir(t *testing.T) {
	// With KUBECONFIG unset, buildK8sClient falls back to $HOME/.kube/config.
	// Pointing HOME at a directory with no .kube/config exercises that
	// fallback-path construction and its resulting (expected) error.
	os.Unsetenv("KUBECONFIG")
	t.Setenv("HOME", t.TempDir())

	if _, err := buildK8sClient(); err == nil {
		t.Fatal("expected an error when $HOME/.kube/config does not exist")
	}
}

// TestHelperMainProcess is not a real test: it's the re-exec target used by
// the TestMainFailsFast* tests below to exercise main()'s early log.Fatal
// paths in a subprocess. main() calls log.Fatal on every startup error,
// which calls os.Exit and would otherwise kill the whole `go test` run, so
// the only safe way to cover those branches is to run main() in a separate
// process and assert on its exit code and output. It only does anything
// when BOLTRUNNER_MAIN_SUBPROCESS is set, so a normal `go test` run treats
// it as a no-op.
func TestHelperMainProcess(t *testing.T) {
	if os.Getenv("BOLTRUNNER_MAIN_SUBPROCESS") != "1" {
		return
	}
	main()
}

func runMainInSubprocess(t *testing.T, env []string) (output string, err error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMainProcess$")
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "BOLTRUNNER_MAIN_SUBPROCESS=1")
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

func TestMainFailsFastWithoutDatabaseURL(t *testing.T) {
	out, err := runMainInSubprocess(t, []string{"DATABASE_URL="})
	if err == nil {
		t.Fatalf("expected main() to exit non-zero without DATABASE_URL, output:\n%s", out)
	}
	if !strings.Contains(out, "DATABASE_URL is required") {
		t.Fatalf("expected a DATABASE_URL error message, got:\n%s", out)
	}
}

func TestMainFailsFastOnUnreachableDatabase(t *testing.T) {
	out, err := runMainInSubprocess(t, []string{
		"DATABASE_URL=postgres://user:pass@127.0.0.1:1/nosuchdb?sslmode=disable&connect_timeout=1",
	})
	if err == nil {
		t.Fatalf("expected main() to exit non-zero with an unreachable database, output:\n%s", out)
	}
	if !strings.Contains(out, "connect to postgres") {
		t.Fatalf("expected a postgres connection error message, got:\n%s", out)
	}
}

func TestMainFailsFastWhenK8sClientCannotBeBuilt(t *testing.T) {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	emptyHome := t.TempDir()
	out, err := runMainInSubprocess(t, []string{
		"DATABASE_URL=" + dsn,
		"KUBECONFIG=",
		"HOME=" + emptyHome,
	})
	if err == nil {
		t.Fatalf("expected main() to exit non-zero when no k8s config is available, output:\n%s", out)
	}
	if !strings.Contains(out, "build k8s client") {
		t.Fatalf("expected a k8s client build error message, got:\n%s", out)
	}
}
