// Package tva reads the provincial system register's published file, systems.json
// (docs/LOOKUP-FORMAT.md in TVA's repository; R-CAL-01, R-CAL-02; B 11). TVA owns a
// system's status and owner: ROSETTA reads them and decides nothing else. Readers
// refuse a format they do not know and ignore fields they do not know (R-LKP-05).
//
// On the server the file comes from TVA's bucket with a read-only key (Bucket, E-ENB-11); locally
// from a folder (Dir), the same file TVA's stand-in is.
package tva

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Format is the one format this reader knows.
const Format = 1

// FileName is the file's name in its folder or bucket.
const FileName = "systems.json"

// The statuses TVA gives. Which of them count is each reader's rule.
const (
	StatusPlanned       = "planned"
	StatusInDevelopment = "in_development"
	StatusMayJoin       = "may_join"
	StatusLive          = "live"
	StatusRetired       = "retired"
)

// ErrFormat says the file is in a format this reader does not know.
var ErrFormat = errors.New("tva: the file is not in a format this reader knows")

// System is one system in the register. Only the fields ROSETTA uses are read.
type System struct {
	Reference    string  `json:"reference"`
	NameSI       *string `json:"name_si"`
	NameTA       *string `json:"name_ta"`
	NameEN       *string `json:"name_en"`
	Status       string  `json:"status"`
	OwnerPostRef *string `json:"owner_post_reference"`
	OwnerSubject *string `json:"owner_subject"`
}

// MayCall reports whether TVA's status lets a system call ROSETTA (R-CAL-01).
func MayCall(status string) bool { return status == StatusLive || status == StatusMayJoin }

// Name is the system's name: English if given, else whichever TVA has. At least
// one is present (LOOKUP-FORMAT.md); the reference stands in if the file breaks that.
func (s System) Name() string {
	for _, n := range []*string{s.NameEN, s.NameSI, s.NameTA} {
		if n != nil && *n != "" {
			return *n
		}
	}
	return s.Reference
}

// Owner is who answers for the system: the subject MJOLNIR knows them by, and
// their post. A null subject is "" and means the flag in R-CAL-02. Readers never
// guess a person from anything else, so nothing here holds an email.
func (s System) Owner() (subject, post string) {
	if s.OwnerSubject != nil {
		subject = *s.OwnerSubject
	}
	if s.OwnerPostRef != nil {
		post = *s.OwnerPostRef
	}
	return subject, post
}

// File is the parsed file.
type File struct {
	Systems []System
}

// Find is the system with this reference. References are opaque: compared for
// equality and nothing else.
func (f File) Find(reference string) (System, bool) {
	for _, s := range f.Systems {
		if s.Reference == reference {
			return s, true
		}
	}
	return System{}, false
}

// Parse reads the file's bytes. An error here never holds the file's contents.
func Parse(raw []byte) (File, error) {
	var head struct {
		Format json.RawMessage `json:"format"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return File{}, errors.New("tva: the file is not JSON")
	}
	if string(head.Format) != fmt.Sprint(Format) {
		return File{}, ErrFormat
	}
	var body struct {
		Systems []System `json:"systems"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return File{}, errors.New("tva: the file does not have format 1's shape")
	}
	for _, s := range body.Systems {
		if s.Reference == "" {
			return File{}, errors.New("tva: a system has no reference")
		}
	}
	return File{Systems: body.Systems}, nil
}

// Reader reads the current file.
type Reader interface {
	Read(ctx context.Context) (File, error)
}

// Dir reads systems.json from a local folder (ROSETTA_TVA_DIR).
type Dir string

// Read reads and parses the file.
func (d Dir) Read(context.Context) (File, error) {
	raw, err := os.ReadFile(filepath.Join(string(d), FileName))
	if err != nil {
		return File{}, fmt.Errorf("tva: %w", err)
	}
	return Parse(raw)
}
