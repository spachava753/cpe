package source_test

import (
	"context"
	"encoding/pem"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acquisition "github.com/spachava753/cpe/internal/source"
)

const checkoutDefinition = "---\nname: alpha\ndescription: Alpha skill\n---\n# Alpha\n"

// TestGitClientFetch exercises the public acquisition boundary with real
// Git processes, temporary repositories, and loopback HTTP servers, not mocks.
func TestGitClientFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	// Never consult the user's Git configuration, netrc, credential programs,
	// or proxies, including libcurl's settings outside Git's own configuration.
	// Subtests run sequentially because t.Setenv changes process-wide settings.
	home := t.TempDir()
	for _, name := range []string{
		"HOME",
		"USERPROFILE",
		"CURL_HOME",
		"XDG_CONFIG_HOME",
	} {
		t.Setenv(
			name,
			home,
		)
	}
	for _, name := range []string{
		"http_proxy",
		"https_proxy",
		"all_proxy",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
	} {
		t.Setenv(
			name,
			"",
		)
	}
	for _, name := range []string{
		"no_proxy",
		"NO_PROXY",
	} {
		t.Setenv(
			name,
			"*",
		)
	}
	netrc := filepath.Join(
		home,
		"empty-netrc",
	)
	if err := os.WriteFile(
		netrc,
		nil,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv(
		"NETRC",
		netrc,
	)
	t.Setenv(
		"GIT_CONFIG_NOSYSTEM",
		"1",
	)
	t.Setenv(
		"GIT_CONFIG_COUNT",
		"0",
	)
	t.Setenv(
		"GIT_CONFIG_PARAMETERS",
		"",
	)
	t.Setenv(
		"GIT_ASKPASS",
		os.DevNull,
	)
	t.Setenv(
		"SSH_ASKPASS",
		os.DevNull,
	)
	configuration := filepath.Join(
		t.TempDir(),
		"gitconfig",
	)
	t.Setenv(
		"GIT_CONFIG_GLOBAL",
		configuration,
	)
	// The source has committed text, binary, and executable files, plus an
	// untracked file that must never appear in an acquired checkout.
	repository := newGitSourceRepository(t)
	locator := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(repository),
	}).String()
	// Exercise accepted remote locators without using any external network. Git
	// rewrites these fixture URLs to a real file:// repository before transport.
	// These cases test locator handling, not real SSH or Git-protocol connections.
	for _, prefix := range []string{
		"https://github.com/fixture/skills",
		"https://github.com/Fixture/Skills",
		"https://github.com/fixture/.github",
		"https://github.com/fixture/skill.tools_v2",
		"https://git.company.invalid/team/subgroup/skills.git",
		"ssh://git@example.com/team/skills.git",
		"git://example.com/team/skills.git",
	} {
		runSourceGit(
			t,
			"config",
			"--file",
			configuration,
			"--add",
			"url."+locator+".insteadOf",
			prefix,
		)
	}
	// Even a validation regression must not send malformed shorthand to GitHub.
	fallback := (&url.URL{
		Scheme: "file",
		Path: filepath.ToSlash(filepath.Join(
			t.TempDir(),
			"missing",
		)),
	}).String()
	runSourceGit(
		t,
		"config",
		"--file",
		configuration,
		"--add",
		"url."+fallback+"/.insteadOf",
		"https://github.com/",
	)
	// Serve a bare repository over "dumb HTTP": update-server-info publishes the
	// indexes Git needs to fetch objects through an ordinary static file server.
	bare := filepath.Join(
		t.TempDir(),
		"skills.git",
	)
	runSourceGit(
		t,
		"clone",
		"--quiet",
		"--bare",
		"--",
		repository,
		bare,
	)
	runSourceGit(
		t,
		"-C",
		bare,
		"update-server-info",
	)
	handler := http.FileServer(http.Dir(filepath.Dir(bare)))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tlsServer := httptest.NewTLSServer(handler)
	t.Cleanup(tlsServer.Close)
	// Trust this fixture's certificate without turning off TLS verification.
	certificate := filepath.Join(
		t.TempDir(),
		"server.pem",
	)
	if err := os.WriteFile(
		certificate,
		pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: tlsServer.Certificate().Raw,
		}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv(
		"GIT_SSL_CAINFO",
		certificate,
	)
	// The 401 fixture tests an authentication failure, not successful login.
	// The isolated environment prevents prompts or access to real credentials.
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(
			"WWW-Authenticate",
			`Basic realm="fixture"`,
		)
		http.Error(
			w,
			"not authorized",
			http.StatusUnauthorized,
		)
	}))
	t.Cleanup(denied.Close)
	// Additional sources cover an empty repository and URL-escaped path names.
	empty := t.TempDir()
	runSourceGit(
		t,
		"init",
		"--quiet",
		"--initial-branch=main",
		empty,
	)
	emptyLocator := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(empty),
	}).String()
	encoded := filepath.Join(
		t.TempDir(),
		"skills #%.git",
	)
	runSourceGit(
		t,
		"clone",
		"--quiet",
		"--bare",
		"--",
		repository,
		encoded,
	)
	encodedLocator := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(encoded),
	}).String()
	transformed := newGitSourceRepository(t)
	if err := os.WriteFile(
		filepath.Join(
			transformed,
			".gitattributes",
		),
		[]byte("SKILL.md text eol=crlf\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	runSourceGit(
		t,
		"-C",
		transformed,
		"add",
		".gitattributes",
		"SKILL.md",
	)
	runSourceGit(
		t,
		"-C",
		transformed,
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.com",
		"commit",
		"--quiet",
		"-m",
		"checkout transformations",
	)
	transformedLocator := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(transformed),
	}).String()
	// Each row changes the source or one acquisition condition. wantErr checks a
	// wrapped cause; errText checks a diagnostic; neither means success is expected.
	for _, test := range []struct {
		name, source, errText                                                 string
		wantErr                                                               error
		wantText                                                              string
		crlf                                                                  bool
		literalBackslash                                                      bool
		reusedPath                                                            bool
		cancel, deadline, duringClone, missingGit, missingTemp, empty, second bool
	}{
		// Accepted locators, with real HTTP(S) transport only for loopback servers.
		{
			name:   "Git tree metadata is independent of CRLF checkout bytes",
			source: transformedLocator,
			crlf:   true,
		},
		{
			name: "Git failure preserves ordinary quotes and backslashes",
			source: (&url.URL{
				Scheme: "file",
				Path: filepath.ToSlash(filepath.Join(
					t.TempDir(),
					`missing "repository" C:\work`,
				)),
			}).String(),
			errText:          "clone source:",
			wantText:         `missing "repository" C:\work`,
			literalBackslash: true,
		},
		{
			name:   "rooted checkout and cleanup",
			source: locator,
		},
		{
			name:   "percent-encoded file path",
			source: encodedLocator,
		},
		{
			name: "localhost file authority",
			source: strings.Replace(
				locator,
				"file:///",
				"file://localhost/",
				1,
			),
		},
		{
			name:   "HTTP Git repository",
			source: server.URL + "/skills.git",
		},
		{
			name:   "HTTPS Git repository",
			source: tlsServer.URL + "/skills.git",
		},
		{
			name:   "GitHub shorthand",
			source: "fixture/skills",
		},
		{
			name:   "case-preserving GitHub shorthand",
			source: "Fixture/Skills",
		},
		{
			name:   "dot repository shorthand",
			source: "fixture/.github",
		},
		{
			name:   "repository punctuation shorthand",
			source: "fixture/skill.tools_v2",
		},
		{
			name:   "enterprise URL",
			source: "https://git.company.invalid/team/subgroup/skills.git",
		},
		{
			name:   "SSH URL",
			source: "ssh://git@example.com/team/skills.git",
		},
		{
			name:   "Git protocol URL",
			source: "git://example.com/team/skills.git",
		},
		// Checkout lifetime and ownership, beyond a single ordinary clone.
		{
			name:   "empty repository",
			source: emptyLocator,
			empty:  true,
		},
		{
			name:   "independent simultaneous checkouts",
			source: locator,
			second: true,
		},
		{
			name:       "repeated cleanup does not delete reused path",
			source:     locator,
			reusedPath: true,
		},
		// Valid inputs whose repositories cannot be acquired.
		{
			name:    "clone failure",
			source:  locator + "-missing",
			errText: "clone",
		},
		{
			name:    "missing remote repository",
			source:  server.URL + "/missing.git",
			errText: "clone",
		},
		{
			name:    "authentication failure without prompting",
			source:  denied.URL + "/skills.git",
			errText: "clone",
		},
		// Invalid locators must fail at parsing, not merely fail later in Git.
		{
			name:    "empty locator",
			errText: "Git URL or GitHub",
		},
		{
			name:    "blank locator",
			source:  " \t",
			errText: "Git URL or GitHub",
		},
		{
			name:    "bare word",
			source:  "not-a-source",
			errText: "Git URL or GitHub",
		},
		{
			name:    "native path is not a Git URL",
			source:  repository,
			errText: "Git URL or GitHub",
		},
		{
			name:    "shorthand parent traversal",
			source:  "fixture/../skills",
			errText: "Git URL or GitHub",
		},
		{
			name:    "encoded separator in shorthand",
			source:  "fixture/skill%2fother",
			errText: "Git URL or GitHub",
		},
		{
			name:    "encoded whitespace in shorthand",
			source:  "fixture/skill%20other",
			errText: "Git URL or GitHub",
		},
		{
			name:    "invalid owner character",
			source:  "fixture_owner/skills",
			errText: "Git URL or GitHub",
		},
		{
			name:    "trailing slash shorthand",
			source:  "fixture/skills/",
			errText: "Git URL or GitHub",
		},
		{
			name:    "malformed URL",
			source:  "http://127.0.0.1:%xx/skills",
			errText: "invalid Git",
		},
		{
			name:    "unsupported scheme",
			source:  "ftp://127.0.0.1/skills",
			errText: "unsupported",
		},
		{
			name:    "URL without host",
			source:  "https:///skills",
			errText: "requires a host",
		},
		{
			name:    "URL without repository",
			source:  "http://127.0.0.1:1/",
			errText: "repository path",
		},
		{
			name:    "URL query",
			source:  "http://127.0.0.1:1/skills?private=fixture",
			errText: "query or fragment",
		},
		{
			name:    "empty URL query",
			source:  "http://127.0.0.1:1/skills?",
			errText: "query or fragment",
		},
		{
			name:    "URL fragment",
			source:  "http://127.0.0.1:1/skills#main",
			errText: "query or fragment",
		},
		{
			name:    "encoded control in URL path",
			source:  "http://127.0.0.1:1/skill%0As",
			errText: "control",
		},
		{
			name:    "URL password rejected without disclosure",
			source:  "http://user:fixture-secret@127.0.0.1:1/skills",
			errText: "password",
		},
		{
			name:    "relative file URL",
			source:  "file://./skills",
			errText: "absolute local",
		},
		{
			name:    "remote file authority",
			source:  "file://other-host/skills",
			errText: "absolute local",
		},
		{
			name: "file URL with user information",
			source: strings.Replace(
				locator,
				"file:///",
				"file://user@localhost/",
				1,
			),
			errText: "file source",
		},
		// Missing runtime dependencies and cancellation at different stages.
		{
			name:       "Git executable unavailable",
			source:     locator,
			missingGit: true,
			wantErr:    exec.ErrNotFound,
		},
		{
			name:        "temporary root unavailable",
			source:      locator,
			missingTemp: true,
			wantErr:     fs.ErrNotExist,
		},
		{
			name:    "cancelled before acquisition",
			source:  locator,
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:     "expired deadline",
			source:   locator,
			deadline: true,
			wantErr:  context.DeadlineExceeded,
		},
		{
			name:        "cancelled during acquisition",
			duringClone: true,
			wantErr:     context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.literalBackslash && filepath.Separator == '\\' {
					t.Skip("fixture needs a literal backslash, not a path separator")
				}
				// Give this acquisition its own resource directory. Environment changes
				// are restored after this subtest, so fault cases do not affect later rows.
				scratch := t.TempDir()
				t.Setenv(
					"TMPDIR",
					scratch,
				)
				if test.missingGit {
					t.Setenv(
						"PATH",
						scratch,
					)
				}
				if test.missingTemp {
					t.Setenv(
						"TMPDIR",
						filepath.Join(
							scratch,
							"missing",
						),
					)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.deadline {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithDeadline(
						ctx,
						time.Unix(
							0,
							0,
						),
					)
					defer deadlineCancel()
				}
				if test.cancel {
					cancel()
				}
				source := test.source
				var resultCheckout acquisition.Checkout
				var fetchErr error
				if test.duringClone {
					// Hold the HTTP response open, then cancel only after Git reaches it.
					// Fetch must return AND its transport must disconnect without a reply.
					// Timeouts are failure guards, not sleeps used to coordinate the test.
					started := make(
						chan struct{},
						1,
					)
					disconnected := make(
						chan struct{},
						1,
					)
					release := make(chan struct{})
					blocked := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
						select {
						case started <- struct{}{}:
						default:
						}
						select {
						case <-request.Context().Done():
							select {
							case disconnected <- struct{}{}:
							default:
							}
						case <-release:
						}
					}))
					defer blocked.Close()
					// Defers run in reverse order: unblock the handler before closing the
					// server, including when an assertion fails while Git is still waiting.
					defer close(release)
					source = blocked.URL + "/skills.git"
					type outcome struct {
						checkout acquisition.Checkout
						err      error
					}
					finished := make(
						chan outcome,
						1,
					)
					go func() {
						checkout, err := (acquisition.GitClient{}).Fetch(
							ctx,
							source,
						)
						finished <- outcome{
							checkout,
							err,
						}
					}()
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						cancel()
						t.Fatal("clone did not reach fixture server")
					}
					cancel()
					select {
					case result := <-finished:
						resultCheckout, fetchErr = result.checkout, result.err
					case <-time.After(5 * time.Second):
						t.Fatal("cancelled clone did not return while server withheld response")
					}
					select {
					case <-disconnected:
					case <-time.After(5 * time.Second):
						t.Error("cancelled transport still owns server connection")
					}
				} else {
					resultCheckout, fetchErr = (acquisition.GitClient{}).Fetch(
						ctx,
						source,
					)
				}
				// Check the operation's error before inspecting failure or success state.
				if test.wantErr != nil {
					if !errors.Is(
						fetchErr,
						test.wantErr,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							fetchErr,
							test.wantErr,
						)
					}
				} else if test.errText != "" {
					if fetchErr == nil || !strings.Contains(
						fetchErr.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v, want %q",
							fetchErr,
							test.errText,
						)
					}
				} else if fetchErr != nil {
					t.Fatal(fetchErr)
				}
				if test.wantText != "" && (fetchErr == nil || !strings.Contains(
					fetchErr.Error(),
					test.wantText,
				)) {
					t.Fatalf(
						"error = %v, want readable text %q",
						fetchErr,
						test.wantText,
					)
				}
				if fetchErr != nil {
					// Failure returns no usable checkout and must not expose the password.
					if resultCheckout.FS != nil || resultCheckout.Cleanup != nil {
						t.Error("failed acquisition returned a checkout")
					}
					if strings.Contains(
						fetchErr.Error(),
						"fixture-secret",
					) {
						t.Error("password disclosed in error")
					}
				} else {
					if resultCheckout.FS == nil || resultCheckout.Cleanup == nil {
						t.Fatal("missing checkout filesystem or cleanup")
					}
					// This defer protects against early test failure. Explicit releases below
					// also let us assert the filesystem's state after cleanup.
					defer func() {
						if err := resultCheckout.Cleanup(); err != nil {
							t.Error(err)
						}
					}()
					// Successful acquisition exposes repository contents through fs.FS.
					// Empty repositories may contain Git's own bookkeeping, but no payload.
					if test.empty {
						entries, err := fs.ReadDir(
							resultCheckout.FS,
							".",
						)
						if err != nil {
							t.Fatal(err)
						}
						for _, entry := range entries {
							if entry.Name() != ".git" {
								t.Errorf(
									"unexpected file in empty source: %s",
									entry.Name(),
								)
							}
						}
					} else {
						for name, want := range map[string]string{
							"SKILL.md":        checkoutDefinition,
							"nested/tool.txt": "support\n",
							"scripts/run.sh":  "echo alpha\n",
							"data.bin": string([]byte{
								0,
								0xff,
								1,
								0x80,
							}),
						} {
							if test.crlf && name == "SKILL.md" {
								want = strings.ReplaceAll(
									want,
									"\n",
									"\r\n",
								)
							}
							data, readErr := fs.ReadFile(
								resultCheckout.FS,
								name,
							)
							if readErr != nil || string(data) != want {
								t.Errorf(
									"%s = %q, %v",
									name,
									data,
									readErr,
								)
							}
						}
						info, statErr := fs.Stat(
							resultCheckout.FS,
							"scripts/run.sh",
						)
						if statErr != nil || info.Mode().Perm()&0o111 == 0 {
							t.Errorf(
								"executable mode lost: %v, %v",
								info,
								statErr,
							)
						}
						if _, readErr := fs.ReadFile(
							resultCheckout.FS,
							"untracked.txt",
						); !errors.Is(
							readErr,
							fs.ErrNotExist,
						) {
							t.Errorf(
								"untracked source file was acquired: %v",
								readErr,
							)
						}
						// io/fs names are source-relative: absolute paths and .. are invalid.
						for _, name := range []string{
							"../outside",
							"/SKILL.md",
						} {
							if _, readErr := fs.ReadFile(
								resultCheckout.FS,
								name,
							); !errors.Is(
								readErr,
								fs.ErrInvalid,
							) {
								t.Errorf(
									"non-source path %q = %v",
									name,
									readErr,
								)
							}
						}
						if _, readErr := fs.ReadDir(
							resultCheckout.FS,
							"nested",
						); readErr != nil {
							t.Fatal(readErr)
						}
					}
					if checkout.GitTreeHash == nil {
						t.Fatal("Git tree metadata is unavailable")
					}
					if !test.empty {
						original := repository
						if test.crlf {
							original = transformed
						}
						for _, directory := range []string{
							".",
							"nested",
							"scripts",
						} {
							object := "HEAD:" + directory
							if directory == "." {
								object = "HEAD^{tree}"
							}
							expected, err := exec.CommandContext(
								t.Context(),
								"git",
								"-C",
								original,
								"rev-parse",
								"--verify",
								object,
							).Output()
							if err != nil {
								t.Fatal(err)
							}
							got, err := checkout.GitTreeHash(
								t.Context(),
								directory,
							)
							if err != nil || got != strings.TrimSpace(string(expected)) {
								t.Fatalf(
									"GitTreeHash(%q) = %q, %v; want %q",
									directory,
									got,
									err,
									expected,
								)
							}
						}
						for _, directory := range []string{
							"../outside",
							"SKILL.md",
							"missing",
						} {
							if _, err := checkout.GitTreeHash(
								t.Context(),
								directory,
							); err == nil {
								t.Fatalf(
									"invalid tree %q was accepted",
									directory,
								)
							}
						}
					}
					// Two acquisitions own separate resources; releasing the first must
					// leave the second readable, even when both came from the same source.
					if test.second {
						other, err := (acquisition.GitClient{}).Fetch(
							t.Context(),
							source,
						)
						if err != nil {
							t.Fatal(err)
						}
						defer func() {
							if err := other.Cleanup(); err != nil {
								t.Error(err)
							}
						}()
						if err := resultCheckout.Cleanup(); err != nil {
							t.Fatal(err)
						}
						data, readErr := fs.ReadFile(
							other.FS,
							"SKILL.md",
						)
						if readErr != nil || string(data) != checkoutDefinition {
							t.Fatal("cleanup affected a different checkout")
						}
						if err := other.Cleanup(); err != nil {
							t.Fatal(err)
						}
					}
					// Capture the owned path before release, then optionally recreate it
					// as someone else's directory. Repeated cleanup must leave it alone.
					var releasedPath string
					if test.reusedPath {
						entries, err := os.ReadDir(scratch)
						if err != nil || len(entries) != 1 {
							t.Fatalf(
								"checkout resources = %v, %v",
								entries,
								err,
							)
						}
						releasedPath = filepath.Join(
							scratch,
							entries[0].Name(),
						)
					}
					if err := resultCheckout.Cleanup(); err != nil {
						t.Fatal(err)
					}
					if test.reusedPath {
						if err := os.Mkdir(
							releasedPath,
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(
							filepath.Join(
								releasedPath,
								"other-owner.txt",
							),
							[]byte("keep"),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
						defer os.RemoveAll(releasedPath)
					}
					if err := resultCheckout.Cleanup(); err != nil {
						t.Errorf(
							"repeated cleanup = %v",
							err,
						)
					}
					if test.reusedPath {
						data, readErr := os.ReadFile(filepath.Join(
							releasedPath,
							"other-owner.txt",
						))
						if readErr != nil || string(data) != "keep" {
							t.Errorf(
								"repeated cleanup deleted unowned resources: %q, %v",
								data,
								readErr,
							)
						}
						if err := os.RemoveAll(releasedPath); err != nil {
							t.Fatal(err)
						}
					}
					// The released checkout is gone, but the original repository and its
					// untracked workspace file must remain untouched.
					if _, readErr := fs.ReadDir(
						resultCheckout.FS,
						".",
					); !errors.Is(
						readErr,
						fs.ErrNotExist,
					) {
						t.Errorf(
							"released checkout still readable: %v",
							readErr,
						)
					}
					for name, want := range map[string]string{
						"SKILL.md":      checkoutDefinition,
						"untracked.txt": "private workspace file",
					} {
						data, readErr := os.ReadFile(filepath.Join(
							repository,
							name,
						))
						if readErr != nil || string(data) != want {
							t.Errorf(
								"cleanup changed original %s: %q, %v",
								name,
								data,
								readErr,
							)
						}
					}
				}
				// Both successful release and failed acquisition must leave no owned
				// temporary resources behind. This check runs for every scenario.
				entries, readErr := os.ReadDir(scratch)
				if readErr != nil || len(entries) != 0 {
					t.Errorf(
						"temporary source resources remain: %v, %v",
						entries,
						readErr,
					)
				}
			},
		)
	}
}

func newGitSourceRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runSourceGit(
		t,
		"init",
		"--quiet",
		"--initial-branch=main",
		repository,
	)
	for name, data := range map[string]string{
		"SKILL.md":        checkoutDefinition,
		"nested/tool.txt": "support\n",
		"scripts/run.sh":  "echo alpha\n",
		"data.bin": string([]byte{
			0,
			0xff,
			1,
			0x80,
		}),
	} {
		filename := filepath.Join(
			repository,
			filepath.FromSlash(name),
		)
		if err := os.MkdirAll(
			filepath.Dir(filename),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
		mode := fs.FileMode(0o600)
		if name == "scripts/run.sh" {
			mode = 0o755
		}
		if err := os.WriteFile(
			filename,
			[]byte(data),
			mode,
		); err != nil {
			t.Fatal(err)
		}
	}
	runSourceGit(
		t,
		"-C",
		repository,
		"add",
		".",
	)
	runSourceGit(
		t,
		"-C",
		repository,
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.com",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	)
	if err := os.WriteFile(
		filepath.Join(
			repository,
			"untracked.txt",
		),
		[]byte("private workspace file"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return repository
}

func runSourceGit(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.CommandContext(
		t.Context(),
		"git",
		args...,
	).CombinedOutput(); err != nil {
		t.Fatalf(
			"git %v: %v, %s",
			args,
			err,
			output,
		)
	}
}
