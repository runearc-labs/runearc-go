package runearc_test

// TestMain starts a real Runearc API and worker against a throwaway Postgres
// database for the test run, then tears both down. Nothing in the mail path is
// mocked: sends go to the platform's simulated test recipients
// (delivered@test.<platform domain>), which the API records as delivered
// without an MTA.
//
// It needs Postgres on localhost (createdb, dropdb and psql on PATH) and the
// Rust binaries, built with:
//
//	CARGO_TARGET_DIR=../../target/agent-sdk cargo build -p runearc-api -p runearc-worker
//
// Override the binary directory with RUNEARC_BIN_DIR.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

// stack is what TestMain started.
var stack struct {
	apiURL       string
	apiKey       string
	dbURL        string
	orgID        string
	domain       string
	domainID     string
	testDomain   string
	sharedDomain string
}

type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 20000 {
		t.buf = t.buf[len(t.buf)-20000:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

type child struct {
	cmd  *exec.Cmd
	log  *tail
	done chan struct{}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func psql(dbURL, sql string) (string, error) {
	out, err := exec.Command("psql", dbURL, "-v", "ON_ERROR_STOP=1", "-tA", "-c", sql).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func startProcess(binDir, bin string, env []string) (*child, error) {
	c := &child{cmd: exec.Command(filepath.Join(binDir, bin)), log: &tail{}, done: make(chan struct{})}
	c.cmd.Env = env
	c.cmd.Stdout = c.log
	c.cmd.Stderr = c.log
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		_ = c.cmd.Wait()
		close(c.done)
	}()
	return c, nil
}

func (c *child) stop() {
	_ = c.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.done
	}
}

func waitHealthy(url string, api *child) error {
	until := time.Now().Add(60 * time.Second)
	for time.Now().Before(until) {
		select {
		case <-api.done:
			return fmt.Errorf("runearc-api exited:\n%s", api.log)
		default:
		}
		if r, err := http.Get(url + "/healthz"); err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("runearc-api did not become healthy:\n%s", api.log)
}

func getJSON(url string, out any) error {
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func setup() (teardown func(), err error) {
	var cleanups []func()
	teardown = func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	defer func() {
		if err != nil {
			teardown()
		}
	}()

	repo, err := filepath.Abs("../..")
	if err != nil {
		return teardown, err
	}
	binDir := os.Getenv("RUNEARC_BIN_DIR")
	if binDir == "" {
		binDir = filepath.Join(repo, "target/agent-sdk/debug")
	}
	for _, bin := range []string{"runearc-api", "runearc-worker"} {
		if _, err := os.Stat(filepath.Join(binDir, bin)); err != nil {
			return teardown, fmt.Errorf("%s is missing. Build it: CARGO_TARGET_DIR=%s cargo build -p runearc-api -p runearc-worker",
				filepath.Join(binDir, bin), filepath.Join(repo, "target/agent-sdk"))
		}
	}

	dbName := "runearc_sdk_test_" + randomHex(6)
	if out, err := exec.Command("createdb", "-h", "localhost", dbName).CombinedOutput(); err != nil {
		return teardown, fmt.Errorf("createdb: %v: %s", err, out)
	}
	cleanups = append(cleanups, func() {
		if os.Getenv("RUNEARC_KEEP_TEST_DB") != "1" {
			_ = exec.Command("dropdb", "-h", "localhost", "--force", "--if-exists", dbName).Run()
		}
	})
	dbURL := "postgres://localhost/" + dbName
	blobDir, err := os.MkdirTemp("", "runearc-sdk-blobs-")
	if err != nil {
		return teardown, err
	}
	cleanups = append(cleanups, func() { _ = os.RemoveAll(blobDir) })
	port, err := freePort()
	if err != nil {
		return teardown, err
	}
	apiURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// A clean environment: none of the developer's RUNEARC_* or KUMO_* settings leak in.
	home := os.Getenv("HOME")
	if home == "" {
		home = os.TempDir()
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	env := []string{
		"PATH=" + path,
		"HOME=" + home,
		"RUST_LOG=warn",
		"DATABASE_URL=" + dbURL,
		"BLOB_URL=file://" + blobDir,
		"RUNEARC_SECRET_KEY=" + randomHex(32),
		fmt.Sprintf("LISTEN_ADDR=127.0.0.1:%d", port),
		"RUNEARC_ALLOW_LOCAL_WEBHOOKS=1",
		// The platform refuses to start without a provider. Test recipients are
		// recorded as delivered without touching it, so this relay is never connected to.
		"RUNEARC_OUTBOUND_PROVIDER=smtp",
		"RUNEARC_SMTP_RELAY=127.0.0.1:1",
	}

	// Bootstrap first: it runs the migrations and prints the key once (never logged here).
	boot := exec.Command(filepath.Join(binDir, "runearc-api"), "bootstrap", "sdk-"+randomHex(3))
	boot.Env = env
	out, err := boot.Output()
	if err != nil {
		return teardown, fmt.Errorf("bootstrap failed: %w", err)
	}
	keyMatch := regexp.MustCompile(`api key \(shown once\): (\S+)`).FindSubmatch(out)
	orgMatch := regexp.MustCompile(`org: (org_\S+)`).FindSubmatch(out)
	if keyMatch == nil || orgMatch == nil {
		return teardown, fmt.Errorf("bootstrap did not print an org and a key")
	}
	apiKey, orgID := string(keyMatch[1]), string(orgMatch[1])
	// Room for the CRUD tests: the free plan allows 3 inboxes and 3 domains.
	if out, err := psql(dbURL, fmt.Sprintf("UPDATE orgs SET plan = 'scale' WHERE id = '%s'", orgID)); err != nil {
		return teardown, fmt.Errorf("psql: %v: %s", err, out)
	}

	api, err := startProcess(binDir, "runearc-api", env)
	if err != nil {
		return teardown, err
	}
	cleanups = append(cleanups, api.stop)
	if err := waitHealthy(apiURL, api); err != nil {
		return teardown, err
	}
	worker, err := startProcess(binDir, "runearc-worker", env)
	if err != nil {
		return teardown, err
	}
	cleanups = append(cleanups, worker.stop)

	var platform struct {
		TestDomain   string `json:"test_domain"`
		SharedDomain string `json:"shared_domain"`
	}
	if err := getJSON(apiURL+"/v1/platform", &platform); err != nil {
		return teardown, err
	}

	// One verified sending domain for the whole run: added through the API,
	// marked verified with one UPDATE (verification needs public DNS).
	domain := "sdk-" + randomHex(4) + ".example.com"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, apiURL+"/v1/domains",
		bytes.NewBufferString(fmt.Sprintf(`{"name":%q}`, domain)))
	if err != nil {
		return teardown, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return teardown, err
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return teardown, fmt.Errorf("adding the test domain failed: %d %s", res.StatusCode, body)
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &added); err != nil {
		return teardown, err
	}
	if out, err := psql(dbURL, fmt.Sprintf("UPDATE domains SET status = 'verified', verified_at = now() WHERE id = '%s'", added.ID)); err != nil {
		return teardown, fmt.Errorf("psql: %v: %s", err, out)
	}

	stack.apiURL, stack.apiKey, stack.dbURL, stack.orgID = apiURL, apiKey, dbURL, orgID
	stack.domain, stack.domainID = domain, added.ID
	stack.testDomain, stack.sharedDomain = platform.TestDomain, platform.SharedDomain
	return teardown, nil
}

func TestMain(m *testing.M) {
	teardown, err := setup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "test setup failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	teardown()
	os.Exit(code)
}

// client is a client for the real API with the bootstrap key.
func client(t testing.TB, opts ...runearc.Option) *runearc.Client {
	t.Helper()
	return clientWithKey(t, stack.apiKey, opts...)
}

func clientWithKey(t testing.TB, key string, opts ...runearc.Option) *runearc.Client {
	t.Helper()
	c, err := runearc.New(key, append([]runearc.Option{runearc.WithBaseURL(stack.apiURL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testAddress is a simulated recipient: delivered (or bounced or complained) without an MTA.
func testAddress(local string) string { return local + "@" + stack.testDomain }

func from() string { return "SDK Tests <app@" + stack.domain + ">" }

func unique(prefix string) string {
	return fmt.Sprintf("%s %d%s", prefix, time.Now().UnixNano(), randomHex(3))
}

// eventually polls fn until it returns true.
func eventually(t testing.TB, d time.Duration, fn func() (bool, error)) {
	t.Helper()
	until := time.Now().Add(d)
	var last error
	for time.Now().Before(until) {
		ok, err := fn()
		if err != nil {
			last = err
		} else if ok {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("condition not met in %s (last error: %v)", d, last)
}

// apiError fails the test unless err is a *runearc.Error.
func apiError(t testing.TB, err error) *runearc.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected the call to fail")
	}
	var e *runearc.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected a *runearc.Error, got %T: %v", err, err)
	}
	return e
}
