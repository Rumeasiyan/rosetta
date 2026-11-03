// Command rosetta-paradoxcall is the local stand-in for PARADOX: it calls
// ROSETTA's job address the way PARADOX does, signed with a test key
// (PARADOX contract, section 5). Development only; never point it at the
// shared server.
//
//	go run ./cmd/rosetta-paradoxcall keygen -out dev/keys/paradox.key
//	go run ./cmd/rosetta-paradoxcall -key dev/keys/paradox.key -url http://localhost:53080 -job audit-catch-up
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "rosetta-paradoxcall:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "keygen" {
		return keygen(args[1:], stdout)
	}
	fs := flag.NewFlagSet("rosetta-paradoxcall", flag.ContinueOnError)
	keyFile := fs.String("key", "", "file holding the test private key (from keygen)")
	url := fs.String("url", "http://localhost:53080", "ROSETTA's address")
	job := fs.String("job", "daily", "job name: nightly, partitions, daily or audit-catch-up")
	runID := fs.String("run-id", "", "run ID (default: a new one); repeat one to test that ROSETTA skips it")
	attempt := fs.Int("attempt", 1, "attempt number")
	timeout := fs.Duration("timeout", 60*time.Second, "how long to wait for ROSETTA's answer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" {
		return errors.New("-key is required")
	}
	raw, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	key, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		return err
	}
	if *runID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		*runID = "dev-" + hex.EncodeToString(b)
	}
	body, _ := json.Marshal(map[string]any{
		"run_id": *runID, "job": *job, "attempt": *attempt,
		"due_at": time.Now().UTC().Format(time.RFC3339), "data": nil,
	})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(*url, "/")+"/jobs/"+*job, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	signer := sign.Signer{KeyID: sign.ParadoxKeyID, Key: key, Direction: sign.ToSystem}
	if err := signer.Sign(req, body); err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: *timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	fmt.Fprintf(stdout, "run %s: %s %s\n", *runID, resp.Status, strings.TrimSpace(string(answer)))
	return nil
}

// keygen writes a new test private key to -out and prints the public half,
// the value ROSETTA's PARADOX_PUBLIC_KEY setting takes.
func keygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "file for the private key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	pub, priv, err := sign.GenerateKey()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(priv+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(stdout, pub)
	return nil
}
