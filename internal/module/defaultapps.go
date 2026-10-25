package module

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// DefaultApps captures the OEM default-app associations XML through DISM.
// Windows 11 has deprecated the import path on recent builds; export still
// works and the XML remains useful as a reference. See README limitations.
type DefaultApps struct{}

// NewDefaultApps returns the default-app associations module.
func NewDefaultApps() *DefaultApps { return &DefaultApps{} }

// DefaultAppsManifest is written to defaultapps.json.
type DefaultAppsManifest struct {
	CapturedAt       time.Time `json:"captured_at"`
	AssociationCount int       `json:"association_count"`
	Format           string    `json:"format"`
}

// DefaultAssociations is the subset of the DISM XML we understand.
type DefaultAssociations struct {
	XMLName      xml.Name             `xml:"DefaultAssociations"`
	Version      string               `xml:"Version,attr,omitempty"`
	Associations []DefaultAssociation `xml:"Association"`
}

// DefaultAssociation is one file/URL association.
type DefaultAssociation struct {
	Identifier      string `xml:"Identifier,attr"`
	ApplicationName string `xml:"ApplicationName,attr"`
	ProgID          string `xml:"ProgId,attr"`
	ApplicationType string `xml:"ApplicationType,attr"`
}

// Name implements core.Module.
func (d *DefaultApps) Name() string { return "default-apps" }

// Description implements core.Module.
func (d *DefaultApps) Description() string {
	return "Default app associations exported with DISM (requires administrator)"
}

// NeedsAdmin implements core.Module.
func (d *DefaultApps) NeedsAdmin() bool { return true }

// Scan implements core.Module.
func (d *DefaultApps) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	if _, err := p.Exec.LookPath("dism"); err != nil {
		return core.Summary{Details: "DISM is unavailable; default-app export requires Windows"}, nil
	}
	return core.Summary{ItemCount: 1, Details: "default app associations (administrator required)"}, nil
}

// Backup implements core.Module.
func (d *DefaultApps) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Exec.LookPath("dism"); err != nil {
		return fmt.Errorf("DISM is not available; default-app capture requires Windows")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	xmlPath, err := filepath.Abs(filepath.Join(dir, "DefaultAppAssociations.xml"))
	if err != nil {
		return err
	}
	p.Reportf("exporting default app associations with DISM")
	res, err := p.Exec.Run(ctx, "dism", "/English", "/Online", "/Export-DefaultAppAssociations:"+xmlPath)
	if err != nil {
		return fmt.Errorf("DISM export failed: %s", commandError(err, res))
	}
	if !fileExists(xmlPath) {
		return fmt.Errorf("DISM did not produce DefaultAppAssociations.xml")
	}
	associations, err := ParseDefaultAssociations(xmlPath)
	if err != nil {
		return err
	}
	manifest := DefaultAppsManifest{
		CapturedAt:       time.Now().UTC(),
		AssociationCount: len(associations.Associations),
		Format:           "dism-default-app-associations",
	}
	if err := writeJSON(filepath.Join(dir, "defaultapps.json"), manifest); err != nil {
		return err
	}
	p.Reportf("captured %d default app association(s)", manifest.AssociationCount)
	return nil
}

// Restore implements core.Module.
func (d *DefaultApps) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	var manifest DefaultAppsManifest
	if err := readJSON(filepath.Join(dir, "defaultapps.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read default-app manifest: %w", err)
	}
	xmlPath := filepath.Join(dir, "DefaultAppAssociations.xml")
	if !fileExists(xmlPath) {
		return fmt.Errorf("DefaultAppAssociations.xml is missing from the backup")
	}
	if opts.DryRun {
		p.Reportf("would import %d default app association(s)", manifest.AssociationCount)
		return nil
	}
	if _, err := p.Exec.LookPath("dism"); err != nil {
		return fmt.Errorf("DISM is not available; default-app restore requires Windows")
	}
	abs, err := filepath.Abs(xmlPath)
	if err != nil {
		return err
	}
	p.Reportf("importing %d default app association(s)", manifest.AssociationCount)
	res, err := p.Exec.Run(ctx, "dism", "/English", "/Online", "/Import-DefaultAppAssociations:"+abs)
	if err != nil {
		message := commandError(err, res)
		if strings.Contains(strings.ToLower(message), "deprecated") || strings.Contains(strings.ToLower(message), "not supported") {
			message += " (Windows 11 removed DISM default-app import on recent builds; the exported XML is still usable as a reference)"
		}
		return fmt.Errorf("DISM import failed: %s", message)
	}
	return nil
}

// Verify implements core.Module.
func (d *DefaultApps) Verify(ctx context.Context, dir string) error {
	var manifest DefaultAppsManifest
	if err := readJSON(filepath.Join(dir, "defaultapps.json"), &manifest); err != nil {
		return fmt.Errorf("default-app manifest: %w", err)
	}
	associations, err := ParseDefaultAssociations(filepath.Join(dir, "DefaultAppAssociations.xml"))
	if err != nil {
		return err
	}
	if manifest.AssociationCount != len(associations.Associations) {
		return fmt.Errorf("default-app manifest count %d does not match %d associations in XML",
			manifest.AssociationCount, len(associations.Associations))
	}
	return nil
}

// ParseDefaultAssociations reads a DISM default-app XML file.
func ParseDefaultAssociations(path string) (DefaultAssociations, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultAssociations{}, fmt.Errorf("read default associations: %w", err)
	}
	var doc DefaultAssociations
	if err := xml.Unmarshal(data, &doc); err != nil {
		return DefaultAssociations{}, fmt.Errorf("parse default associations: %w", err)
	}
	if doc.XMLName.Local != "DefaultAssociations" {
		return DefaultAssociations{}, fmt.Errorf("unexpected default associations root element %q", doc.XMLName.Local)
	}
	for i := range doc.Associations {
		doc.Associations[i].Identifier = strings.TrimSpace(doc.Associations[i].Identifier)
	}
	return doc, nil
}
