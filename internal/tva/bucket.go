package tva

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DefaultBucket is where TVA publishes its lookup files (docs/LOOKUP-FORMAT.md).
const DefaultBucket = "tva-lookup"

// maxFile bounds the file: the register holds at most a few thousand rows.
const maxFile = 32 << 20

// emptyBody is the SHA-256 of nothing, which a GET signs.
const emptyBody = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Bucket reads systems.json from TVA's bucket with ROSETTA's own read-only key, signed as S3
// expects. It asks for that one file and never lists. Before TVA's first daily run there is no
// file, which answers as a failed read: readers keep what they hold (LOOKUP-FORMAT.md).
type Bucket struct {
	Endpoint string // such as http://caddy:8333
	Name     string
	Key      string // never logged
	Secret   string // never logged
	Region   string
	HTTP     *http.Client
	Now      func() time.Time
}

// Read reads and parses the file.
func (b Bucket) Read(ctx context.Context) (File, error) {
	u := strings.TrimRight(b.Endpoint, "/") + "/" + url.PathEscape(b.Name) + "/" + FileName
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return File{}, errors.New("tva: the bucket's address is not usable")
	}
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	sign(req, b.Key, b.Secret, b.Region, now().UTC())
	client := b.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return File{}, errors.New("tva: the bucket could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return File{}, fmt.Errorf("tva: the bucket answered status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxFile))
	if err != nil {
		return File{}, errors.New("tva: the file was cut short")
	}
	return Parse(raw)
}

// FromEnv is the reader the settings name: TVA's bucket when ROSETTA_TVA_S3_ENDPOINT is set, the
// local folder ROSETTA_TVA_DIR otherwise. Pass os.Getenv.
func FromEnv(getenv func(string) string) (Reader, error) {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	if endpoint := get("ROSETTA_TVA_S3_ENDPOINT"); endpoint != "" {
		b := Bucket{Endpoint: endpoint, Name: get("ROSETTA_TVA_S3_BUCKET"), Key: get("ROSETTA_TVA_S3_KEY"),
			Secret: get("ROSETTA_TVA_S3_SECRET"), Region: get("ROSETTA_TVA_S3_REGION"), HTTP: &http.Client{}}
		if u, err := url.Parse(endpoint); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, errors.New("ROSETTA_TVA_S3_ENDPOINT is not an http or https address")
		}
		if b.Key == "" || b.Secret == "" {
			return nil, errors.New("ROSETTA_TVA_S3_KEY and ROSETTA_TVA_S3_SECRET are not set (the system register's file; see .env.example)")
		}
		if b.Name == "" {
			b.Name = DefaultBucket
		}
		if b.Region == "" {
			b.Region = "us-east-1"
		}
		return b, nil
	}
	if dir := get("ROSETTA_TVA_DIR"); dir != "" {
		return Dir(dir), nil
	}
	return nil, errors.New("neither ROSETTA_TVA_S3_ENDPOINT nor ROSETTA_TVA_DIR is set (the system register's file; see .env.example)")
}

// sign adds an AWS Signature Version 4 to a GET, covering every header it carries.
func sign(req *http.Request, key, secret, region string, at time.Time) {
	stamp, day := at.Format("20060102T150405Z"), at.Format("20060102")
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", emptyBody)
	headers := map[string]string{"host": req.URL.Host}
	names := []string{"host"}
	for k, v := range req.Header {
		l := strings.ToLower(k)
		headers[l] = strings.TrimSpace(strings.Join(v, ","))
		names = append(names, l)
	}
	sort.Strings(names)
	var canonical strings.Builder
	for _, k := range names {
		canonical.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	request := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.Query().Encode(), canonical.String(), signed, emptyBody}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(request))
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	k := mac([]byte("AWS4"+secret), day)
	for _, part := range []string{region, "s3", "aws4_request"} {
		k = mac(k, part)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+key+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(k, toSign)))
}

func mac(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
