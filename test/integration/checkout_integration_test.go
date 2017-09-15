package integration

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lox/bintest"
)

func TestCheckingOutLocalGitProject(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-vv",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fd",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]interface{}{
		{"clone", "-vv", "--", tester.Repo.Path, "."},
		{"clean", "-fd"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fd"},
		{"fetch", "-v", "origin", "master"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"submodule", "sync", "--recursive"},
		{"submodule", "update", "--init", "--recursive", "--force"},
		{"submodule", "foreach", "--recursive", "git", "reset", "--hard"},
		{"clean", "-fd"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fd"},
		{"show", "HEAD", "-s", "--format=fuller", "--no-color"},
		{"branch", "--contains", "HEAD", "--no-color"},
	})

	// required by debug mode
	git.Expect("--version").
		AndWriteToStdout(`git version 2.13.3`).
		AndExitWith(0)

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(1)
	agent.
		Expect("meta-data", "set", "buildkite:git:commit", bintest.MatchAny()).
		AndExitWith(0)
	agent.
		Expect("meta-data", "set", "buildkite:git:branch", bintest.MatchAny()).
		AndExitWith(0)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithSSHFingerprintVerification(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keygen").
		Expect("-f", bintest.MatchAny(), "-F", "github.com").
		AndExitWith(0)

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		AndExitWith(0)

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=true`,
	}

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithoutSSHFingerprintVerification(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		NotCalled()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=false`,
	}

	tester.RunAndCheck(t, env...)

	if !strings.Contains(tester.Output, `Skipping auto SSH fingerprint verification`) {
		t.Fatalf("Expected output")
	}
}

func TestCleaningAnExistingCheckout(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// The checkout dir shouldn't be removed first
	tester.MustMock(t, "rm").Expect("-rf", tester.CheckoutDir()).NotCalled()

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf("Clone failed with %s", out)
	}
	err = ioutil.WriteFile(filepath.Join(tester.CheckoutDir(), "test.txt"), []byte("llamas"), 0700)
	if err != nil {
		t.Fatalf("Write failed with %s", out)
	}

	t.Logf("Wrote %s", filepath.Join(tester.CheckoutDir(), "test.txt"))
	tester.RunAndCheck(t)

	_, err = os.Stat(filepath.Join(tester.CheckoutDir(), "test.txt"))
	if os.IsExist(err) {
		t.Fatalf("test.txt still exitst")
	}
}

func TestForcingACleanCheckout(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "rm").Expect("-rf", tester.CheckoutDir())
	tester.RunAndCheck(t, "BUILDKITE_CLEAN_CHECKOUT=true")
}
