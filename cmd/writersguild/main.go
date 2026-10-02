// Command writersguild runs the Writers' Guild server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"writersguild/guide"
	"writersguild/internal/api"
	"writersguild/internal/auth"
	"writersguild/internal/config"
	"writersguild/internal/db"
	"writersguild/internal/db/sqlcgen"
	guildpkg "writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/seed"
	"writersguild/web"
)

func main() {
	resetAdmin := flag.Bool("reset-admin-password", false, "set the APP_USERNAME account's password from APP_PASSWORD and exit")
	healthcheck := flag.Bool("healthcheck", false, "probe the running server on PORT and exit 0 when healthy")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log, *resetAdmin, *healthcheck); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, resetAdmin, healthcheck bool) error {
	if healthcheck {
		return probe()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	q := sqlcgen.New(pool)

	if resetAdmin {
		return resetAdminPassword(ctx, q, cfg, log)
	}
	if err := bootstrap(ctx, q, cfg, log); err != nil {
		return err
	}
	if n, err := q.FailStaleRuns(ctx); err != nil {
		return fmt.Errorf("mark stale runs: %w", err)
	} else if n > 0 {
		log.Warn("marked runs left over from a previous start as failed", "count", n)
	}

	secret := cfg.SessionSecret
	if secret == "" {
		secret = randomSecret()
		log.Warn("SESSION_SECRET is not set; sessions will not survive a restart. Set it in .env (32+ random characters).")
	}
	signer, err := auth.NewSigner(secret)
	if err != nil {
		return err
	}
	client := llm.NewLiteLLM(cfg.LiteLLMBaseURL, cfg.LiteLLMAPIKey)
	tracker := runs.NewTracker(q, client, cfg.AppName)
	engine := runs.NewEngine(tracker, q, cfg.RunTimeout, log)
	g := guildpkg.New(engine, q, cfg.AppName, cfg.LLMTimeout)
	server := api.NewServer(cfg, pool, q, client, g, engine, api.Auth{Signer: signer, Limiter: auth.NewLimiter(), MaxAge: cfg.SessionMaxAge, CookieSecure: cfg.CookieSecure}, log)

	webFS, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}
	guideFS, err := fs.Sub(guide.Dist, "dist")
	if err != nil {
		return err
	}
	handler := api.NewRouter(server, webFS, guideFS)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "port", cfg.Port, "gateway", cfg.LiteLLMBaseURL, "app_name", cfg.AppName)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if eerr := engine.Shutdown(shutdownCtx); eerr != nil {
			log.Warn("runs still active at shutdown; they are marked failed at the next start", "err", eerr)
		}
		return err
	}
}

// bootstrap creates the first account from the environment when the users
// table is empty, and seeds its writers. An installation that had a single
// user before accounts arrived keeps everything: that user becomes the admin.
func bootstrap(ctx context.Context, q *sqlcgen.Queries, cfg config.Config, log *slog.Logger) error {
	n, err := q.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n == 1 {
		only, err := q.GetFirstUser(ctx)
		if err != nil {
			return fmt.Errorf("load the only user: %w", err)
		}
		if only.Role != "admin" {
			if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: only.ID, Role: "admin"}); err != nil {
				return fmt.Errorf("promote the only user: %w", err)
			}
			log.Info("the only existing account is now the admin", "username", only.Username)
		}
	}
	if n > 0 {
		return nil
	}
	if cfg.AppUsername == "" || cfg.AppPassword == "" {
		return errors.New("no accounts exist yet: set APP_USERNAME and APP_PASSWORD to create the first admin")
	}
	if err := auth.ValidateUsername(cfg.AppUsername); err != nil {
		return fmt.Errorf("APP_USERNAME: %w", err)
	}
	if err := auth.ValidatePassword(cfg.AppPassword); err != nil {
		return fmt.Errorf("APP_PASSWORD: %w", err)
	}
	hash, err := auth.HashPassword(cfg.AppPassword)
	if err != nil {
		return err
	}
	u, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{Username: cfg.AppUsername, DisplayName: "", PasswordHash: hash, Role: "admin"})
	if err != nil {
		return fmt.Errorf("create first account: %w", err)
	}
	if err := seed.ForUser(ctx, q, u.ID, cfg.AppName, cfg.DefaultModelAlias); err != nil {
		return err
	}
	log.Info("created the first account", "username", u.Username, "role", u.Role)
	return nil
}

// resetAdminPassword sets the password of the APP_USERNAME account from
// APP_PASSWORD; every existing session of that account stops working.
func resetAdminPassword(ctx context.Context, q *sqlcgen.Queries, cfg config.Config, log *slog.Logger) error {
	if cfg.AppUsername == "" || cfg.AppPassword == "" {
		return errors.New("set APP_USERNAME and APP_PASSWORD to reset a password")
	}
	if err := auth.ValidatePassword(cfg.AppPassword); err != nil {
		return fmt.Errorf("APP_PASSWORD: %w", err)
	}
	u, err := q.GetUserByUsername(ctx, cfg.AppUsername)
	if err != nil {
		return fmt.Errorf("no account named %q", cfg.AppUsername)
	}
	hash, err := auth.HashPassword(cfg.AppPassword)
	if err != nil {
		return err
	}
	if err := q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{ID: u.ID, PasswordHash: hash}); err != nil {
		return err
	}
	log.Info("password reset; all sessions of the account were signed out", "username", u.Username)
	return nil
}

func probe() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// randomSecret makes a one-process session secret for installations that
// set none.
func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
