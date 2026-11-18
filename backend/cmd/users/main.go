package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"velmir/backend/internal/auth"
	"velmir/backend/internal/config"
)

const userCommandTimeout = 30 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("user command failed: %v", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: users create|assign-role|list")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), userCommandTimeout)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("open admin database: %w", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}

	switch args[0] {
	case "create":
		return createUser(ctx, db, output, args[1:])
	case "assign-role":
		return assignRole(ctx, db, output, args[1:])
	case "list":
		return listUsers(ctx, db, output, args[1:])
	default:
		return errors.New("unknown command; usage: users create|assign-role|list")
	}
}

func createUser(ctx context.Context, db auth.DBTX, output io.Writer, args []string) error {
	flags := flag.NewFlagSet("users create", flag.ContinueOnError)
	email := flags.String("email", "", "user email")
	displayName := flags.String("display-name", "", "display name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: users create -email EMAIL [-display-name NAME]; password is read from VELMIR_USER_PASSWORD")
	}
	password := os.Getenv("VELMIR_USER_PASSWORD")
	if password == "" {
		return errors.New("VELMIR_USER_PASSWORD is required; do not pass the password as a command-line argument")
	}
	user, err := auth.CreateUser(ctx, db, *email, *displayName, password)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Created user %s (id %d) with no assigned roles\n", user.Email, user.ID)
	return err
}

func assignRole(ctx context.Context, db auth.TxBeginner, output io.Writer, args []string) error {
	flags := flag.NewFlagSet("users assign-role", flag.ContinueOnError)
	email := flags.String("email", "", "user email")
	role := flags.String("role", "", "existing role name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: users assign-role -email EMAIL -role ROLE")
	}
	if err := auth.AssignRole(ctx, db, *email, *role); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "Assigned role %s to %s\n", strings.TrimSpace(*role), auth.NormalizeEmail(*email))
	return err
}

func listUsers(ctx context.Context, db auth.QueryDBTX, output io.Writer, args []string) error {
	flags := flag.NewFlagSet("users list", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: users list")
	}
	users, err := auth.ListUsers(ctx, db)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "ID\tEMAIL\tDISPLAY NAME\tACTIVE\tROLES"); err != nil {
		return err
	}
	for _, user := range users {
		roles := strings.Join(user.Roles, ",")
		if roles == "" {
			roles = "(none)"
		}
		if _, err := fmt.Fprintf(output, "%d\t%s\t%s\t%t\t%s\n", user.ID, user.Email, user.DisplayName, user.IsActive, roles); err != nil {
			return err
		}
	}
	return nil
}
