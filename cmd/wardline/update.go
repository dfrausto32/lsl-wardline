package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/update"
)

const (
	defaultUpdateRepo = "dfrausto32/lsl-wardline"
	defaultKeyFile    = "/etc/wardline/release.pub"
)

type updateOpts struct {
	repo, keyFile, apiBase string
	minAge                 time.Duration
}

func (o *updateOpts) config() (*update.Config, error) {
	raw, err := os.ReadFile(o.keyFile)
	if err != nil {
		return nil, fmt.Errorf("could not read the release public key at %s: %w", o.keyFile, err)
	}
	key, err := update.ParsePublicKey(string(raw))
	if err != nil {
		return nil, err
	}
	return &update.Config{Repo: o.repo, APIBase: o.apiBase, PublicKey: key, Current: version, MinAge: o.minAge}, nil
}

// updateCmd lets a server keep itself up to date. It only ever installs a release whose checksum list is
// signed with the owner's key, and puts the old version back if the new one does not start properly.
func updateCmd() *cobra.Command {
	o := &updateOpts{}
	c := &cobra.Command{Use: "update", Short: "Keep this computer up to date from signed releases"}
	c.PersistentFlags().StringVar(&o.repo, "repo", envOr("WARDLINE_UPDATE_REPO", defaultUpdateRepo), "GitHub repository to take releases from (owner/name)")
	c.PersistentFlags().StringVar(&o.keyFile, "key-file", envOr("WARDLINE_UPDATE_KEY", defaultKeyFile), "file holding the public key releases must be signed with")
	c.PersistentFlags().StringVar(&o.apiBase, "api-base", "", "")
	c.PersistentFlags().MarkHidden("api-base")
	c.PersistentFlags().DurationVar(&o.minAge, "min-age", time.Hour, "how long a release must have been out before it is installed")

	c.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Say whether a newer release is ready to install, without installing it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := o.config()
			if err != nil {
				return err
			}
			return report(cmd.OutOrStdout(), version, func() (*update.Release, error) { return cfg.Check(cmd.Context()) })
		},
	}, &cobra.Command{
		Use:   "run",
		Short: "Install the newest signed release if there is one (run by the update timer, as root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cfg, err := o.config()
			if err != nil {
				return err
			}
			host, paths, err := systemHost()
			if err != nil {
				return err
			}
			var rel *update.Release
			if err := report(out, version, func() (*update.Release, error) {
				r, err := cfg.Check(cmd.Context())
				rel = r
				return r, err
			}); err != nil || rel == nil {
				return err
			}
			if update.IsKnownBad(paths, rel.Tag) {
				fmt.Fprintf(out, "%s failed to start here before, so it is not being tried again. Delete %s/bad-%s to try it again.\n", rel.Tag, paths.StateDir, rel.Tag)
				return nil
			}
			dir, err := os.MkdirTemp("", "wardline-download-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()
			tarball, err := cfg.Download(ctx, rel, dir)
			if err != nil {
				return fmt.Errorf("not installing %s: %w", rel.Tag, err)
			}
			fmt.Fprintf(out, "Installing %s (signature and checksum are good).\n", rel.Tag)
			if err := update.Apply(ctx, tarball, rel.Tag, paths, host); err != nil {
				return err
			}
			fmt.Fprintf(out, "Now running %s.\n", rel.Tag)
			return nil
		},
	})
	return c
}

// report prints what a check found. Having nothing to do is not an error.
func report(out io.Writer, current string, check func() (*update.Release, error)) error {
	rel, err := check()
	switch {
	case err == nil:
		fmt.Fprintf(out, "%s is ready to install (this is %s).\n", rel.Tag, current)
		return nil
	case errors.Is(err, update.ErrNothingNew):
		fmt.Fprintf(out, "Up to date (%s).\n", current)
		return nil
	case errors.Is(err, update.ErrTooFresh):
		fmt.Fprintln(out, "A newer release exists but it is still in its waiting period. Nothing was installed.")
		return nil
	}
	return err
}
