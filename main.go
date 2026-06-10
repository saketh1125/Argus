// Command codesentinel runs the autonomous multi-agent bug-detection pipeline:
// it ingests a GitHub repository, localizes and analyzes suspicious code,
// generates and validates fixes, and raises pull requests — with a live
// dashboard showing progress.
//
// Every external integration (LLM, embeddings, vector DB, sandbox, GitHub)
// degrades to a deterministic offline adapter when its credential/service is
// absent, so the full pipeline runs end-to-end with zero keys.
//
//	go run . --repo https://github.com/owner/repo
//	go run . --repo ./testdata/sample-repo        # local path (mock clone)
//	FORCE_MOCK=true go run .                       # fully offline demo
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/saketh/codesentinel/agents"
	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/dashboard"
	"github.com/saketh/codesentinel/models"
	"github.com/saketh/codesentinel/services"
	"github.com/saketh/codesentinel/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n\x1b[31mcodesentinel: %v\x1b[0m\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	repoFlag := flag.String("repo", "", "GitHub repository URL (or local path) to analyze")
	mockFlag := flag.Bool("mock", false, "force all integrations into deterministic offline mode")
	noDash := flag.Bool("no-dashboard", false, "disable the live web dashboard")
	webFlag := flag.Bool("web", false, "open browser launcher — configure and start from the GUI")
	flag.Parse()

	if *repoFlag != "" {
		cfg.RepoURL = *repoFlag
	}
	if *mockFlag {
		cfg.ForceMock = true
	}
	if *noDash {
		cfg.NoDashboard = true
	}

	// Root context, cancelled on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()

	// --- Dashboard / reporter (started before pipeline in --web mode). ---
	var rep models.Reporter = models.NoopReporter{}
	var dash *dashboard.Dashboard
	if !cfg.NoDashboard {
		dash = dashboard.New(cfg.DashboardPort)
		if e := dash.Start(); e != nil {
			fmt.Fprintf(os.Stderr, "warning: dashboard disabled (%v)\n", e)
			dash = nil
		} else {
			rep = dash
		}
	}

	// --- Web launcher: wait for the browser form before running the pipeline. ---
	if *webFlag && dash != nil {
		launchCh := dash.ListenForLaunch()
		url := dash.URL()
		fmt.Printf("\x1b[36m▶ Launcher:\x1b[0m %s\n", url)
		fmt.Println("  Open the URL above, fill in your credentials, and click Start Analysis.")
		openBrowser(url)

		select {
		case lc := <-launchCh:
			applyLaunchConfig(cfg, lc)
			if cfg.RepoURL == "" {
				cfg.RepoURL = "./testdata/sample-repo"
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	} else {
		// CLI mode: apply repo default if still unset.
		if cfg.RepoURL == "" {
			cfg.RepoURL = "./testdata/sample-repo"
		}
		if dash != nil {
			fmt.Printf("\x1b[36m▶ Dashboard:\x1b[0m %s\n", dash.URL())
		}
	}

	// --- Wire external services (real or mock, auto-selected). ---
	clients, err := services.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("init services: %w", err)
	}
	gh := tools.NewGitHub(cfg)
	parser := tools.NewParser(cfg)

	banner(cfg, clients, gh)

	// Continuously surface rate-limiter usage on the dashboard.
	go pumpRPM(ctx, rep, clients.Limiter)

	// --- Pipeline. ---
	pre := agents.NewPreprocessor(gh, parser, clients.Embed, clients.Vectors, cfg, rep)
	idx, workdir, owner, repo, err := pre.Run(ctx, cfg.RepoURL)
	if err != nil {
		return fmt.Errorf("preprocess: %w", err)
	}
	defer os.RemoveAll(workdir)
	fmt.Printf("  indexed %d files from %s/%s\n", idx.FileCount(), owner, repo)

	base, err := gh.DefaultBranch(ctx, owner, repo)
	if err != nil || base == "" {
		base = "main"
	}

	loc := agents.NewLocalizer(clients.LLM, clients.Embed, clients.Vectors, cfg, rep)
	targets, err := loc.Run(ctx, idx)
	if err != nil {
		return fmt.Errorf("localize: %w", err)
	}
	fmt.Printf("  localized %d suspicious targets\n", len(targets))

	ana := agents.NewAnalyst(clients.LLM, cfg, rep)
	issues, err := ana.Run(ctx, idx, targets)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	fmt.Printf("  analysts surfaced %d candidate issues\n", len(issues))

	agg := agents.NewAggregator(cfg, rep)
	forFix, summary := agg.Run(idx, issues)
	fmt.Printf("  %d issues passed the severity/confidence gate; %d → summary\n", len(forFix), len(summary))

	fix := agents.NewFixGenerator(clients.LLM, cfg, rep)
	fixed, err := fix.Run(ctx, idx, forFix)
	if err != nil {
		return fmt.Errorf("fix generation: %w", err)
	}

	val := agents.NewValidator(clients.Sandbox, clients.LLM, parser, cfg, rep)
	validated, err := val.Run(ctx, workdir, fixed)
	if err != nil {
		return fmt.Errorf("validation: %w", err)
	}

	pr := agents.NewPRAgent(gh, cfg, rep)
	report, err := pr.Run(ctx, owner, repo, base, validated, summary)
	if err != nil {
		return fmt.Errorf("pr creation: %w", err)
	}
	report.RepoURL = cfg.RepoURL
	report.FilesAnalyzed = idx.FileCount()
	report.Mode = clients.Mode
	report.DurationMillis = time.Since(start).Milliseconds()

	printReport(report)

	// Keep the dashboard alive so a human can view the final state.
	if dash != nil {
		fmt.Printf("\n\x1b[36mDashboard live at %s — press Ctrl-C to exit.\x1b[0m\n", dash.URL())
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = dash.Stop(shutCtx)
	}
	return nil
}

func banner(cfg *config.Config, c *services.Clients, gh tools.GitHubClient) {
	fmt.Println("\n\x1b[1m🛡  CodeSentinel\x1b[0m — autonomous multi-agent bug detection")
	fmt.Printf("  mode=%s  llm=%s  embed=%s  vectors=%s  sandbox=%s  github=%s\n",
		c.Mode, c.LLM.Name(), c.Embed.Name(), c.Vectors.Name(), c.Sandbox.Name(), gh.Name())
	fmt.Printf("  repo=%s  max_files=%d  rpm_ceiling=%d  confidence_gate=%.2f\n\n",
		cfg.RepoURL, cfg.MaxFiles, cfg.MaxRPM, cfg.ConfidenceGate)
}

// applyLaunchConfig merges browser form values into cfg.
func applyLaunchConfig(cfg *config.Config, lc dashboard.LaunchConfig) {
	if lc.RepoURL != "" {
		cfg.RepoURL = lc.RepoURL
	}
	if lc.GitHubToken != "" {
		cfg.GitHubToken = lc.GitHubToken
	}
	if lc.NIMAPIKey != "" {
		cfg.NIMAPIKey = lc.NIMAPIKey
	}
	if lc.E2BAPIKey != "" {
		cfg.E2BAPIKey = lc.E2BAPIKey
	}
	if lc.OllamaHost != "" {
		cfg.OllamaHost = lc.OllamaHost
	}
	if lc.QdrantHost != "" {
		cfg.QdrantHost = lc.QdrantHost
	}
	if lc.QdrantPort > 0 {
		cfg.QdrantPort = lc.QdrantPort
	}
	if lc.EmbedModel != "" {
		cfg.EmbedModel = lc.EmbedModel
	}
	if lc.ForceMock {
		cfg.ForceMock = true
	}
	if lc.MaxFiles > 0 {
		cfg.MaxFiles = lc.MaxFiles
	}
	if lc.ConfidenceGate > 0 {
		cfg.ConfidenceGate = lc.ConfidenceGate
	}
}

// openBrowser tries to open url in the default browser. Failure is silent.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default: // linux and others
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

func pumpRPM(ctx context.Context, rep models.Reporter, lim services.RateLimiter) {
	if lim == nil {
		return
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur, max := lim.Snapshot()
			rep.SetRPM(cur, max)
		}
	}
}

func printReport(r *models.RunReport) {
	fmt.Println("\n\x1b[1m── Run Report ──────────────────────────────\x1b[0m")
	fmt.Printf("  repo:           %s\n", r.RepoURL)
	fmt.Printf("  mode:           %s\n", r.Mode)
	fmt.Printf("  files analyzed: %d\n", r.FilesAnalyzed)
	fmt.Printf("  issues found:   %d\n", r.IssuesFound)
	fmt.Printf("  PRs raised:     %d\n", len(r.PRsRaised))
	for _, p := range r.PRsRaised {
		fmt.Printf("    • #%d %s\n", p.Number, p.URL)
	}
	if r.SummaryIssue != "" {
		fmt.Printf("  summary issue:  %s\n", r.SummaryIssue)
	}
	fmt.Printf("  demoted:        %d\n", len(r.DemotedIssues))
	fmt.Printf("  duration:       %dms\n", r.DurationMillis)
	fmt.Println("\x1b[1m────────────────────────────────────────────\x1b[0m")
}
