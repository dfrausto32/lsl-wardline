package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/api"
	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/auth"
	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/webui"
)

// controlOpts are the flags the control plane commands share.
type controlOpts struct {
	listen, data, publicURL, level string
}

// defaultControlDir is where the control plane keeps its database and secret key.
func defaultControlDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(programData(), "Wardline-control")
	}
	return "/var/lib/wardline"
}

func controlCmd() *cobra.Command {
	o := &controlOpts{}
	c := &cobra.Command{
		Use:   "control",
		Short: "Run the control plane: the web app, the API and the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Started by the Windows service manager, it runs under that instead.
			if handled, err := runAsService(controlServiceName, func(ctx context.Context) error { return serveControl(ctx, o) }); handled {
				return err
			}
			return serveControl(cmd.Context(), o)
		},
	}
	f := c.PersistentFlags() // subcommands such as install share these
	f.StringVar(&o.listen, "listen", envOr("WARDLINE_LISTEN", ":8080"), "address to listen on")
	f.StringVar(&o.data, "data", envOr("WARDLINE_DATA", defaultControlDir()), "folder for the database and secret key")
	f.StringVar(&o.publicURL, "public-url", envOr("WARDLINE_PUBLIC_URL", ""), "the address servers use to reach this control plane")
	f.StringVar(&o.level, "log-level", envOr("WARDLINE_LOG_LEVEL", "info"), "debug, info, warn or error")
	platformControlCommands(c, o)
	return c
}

func serveControl(ctx context.Context, o *controlOpts) error {
	listen, data, publicURL := o.listen, o.data, o.publicURL
	log := wlog.New(os.Stderr, o.level)
	if err := os.MkdirAll(data, 0o700); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(data, "wardline.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	key, err := auth.LoadOrCreateKey(filepath.Join(data, "secret.key"))
	if err != nil {
		return err
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		return err
	}
	sk, err := api.EnsureSigningKey(ctx, st, box)
	if err != nil {
		return err
	}
	if publicURL == "" {
		publicURL = "http://localhost" + listen
		log.Warn("no public URL set; servers will be told to reach this control plane at " + publicURL)
	}
	srv := api.New(api.Deps{
		Store: st, Audit: audit.New(st.DB()), Box: box, Log: log, SigningKey: sk, PublicURL: publicURL,
		SecureCookies: strings.HasPrefix(publicURL, "https://"), Static: webui.Handler(),
	})
	hs := &http.Server{Addr: listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sctx)
	}()
	log.Info("control plane listening", "address", listen, "public_url", publicURL)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func auditCmd() *cobra.Command {
	var data string
	root := &cobra.Command{Use: "audit", Short: "Check or export the Activity log"}
	open := func() (*store.Store, error) {
		path := filepath.Join(data, "wardline.db")
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("no database found in %s", data)
		}
		return store.Open(path)
	}
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Check that no Activity entry has been changed or removed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := audit.New(st.DB()).Verify(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Activity log is intact: %d entries checked.\n", n)
			return nil
		},
	}
	export := &cobra.Command{
		Use:   "export",
		Short: "Write the Activity log as CSV to standard output",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			return audit.New(st.DB()).ExportCSV(cmd.Context(), cmd.OutOrStdout(), audit.Filter{})
		},
	}
	root.PersistentFlags().StringVar(&data, "data", envOr("WARDLINE_DATA", "/var/lib/wardline"), "folder with the database")
	root.AddCommand(verify, export)
	return root
}
