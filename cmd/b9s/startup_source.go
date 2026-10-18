package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/ui"
)

// startupSource is the live-reload source of the startup project.
type startupSource struct {
	sourceType datasource.SourceType
	watcher    *datasource.DoltWatcher
	source     datasource.DataSource
	failure    *ui.DoltFailure
	info       string
}

// stop releases the watcher of a source the startup project does not use.
func (s startupSource) stop() {
	if s.watcher != nil {
		s.watcher.Stop()
	}
}

// connectStartupSource detects the source type of beadsDir and starts a Dolt
// watcher when it has one. When the Dolt server cannot be reached it falls
// back to JSONL or SQLite and records the failure for the health popup.
func connectStartupSource(beadsDir, beadsPath string, pollInterval time.Duration) startupSource {
	detectedSourceType := datasource.SourceTypeJSONLLocal
	var doltWatcher *datasource.DoltWatcher
	var doltSource datasource.DataSource
	var doltFailure *ui.DoltFailure
	sourceInfo := ""
	if sources, discErr := datasource.DiscoverSources(datasource.DiscoveryOptions{
		BeadsDir:               beadsDir,
		ValidateAfterDiscovery: false,
	}); discErr == nil {
		debug.Log("datasource: discovered %d sources", len(sources))
		for i, s := range sources {
			debug.Log("datasource: source[%d] type=%s path=%s db=%s user=%s", i, s.Type, s.Path, s.Database, s.User)
			if s.Type == datasource.SourceTypeDolt {
				db := s.Database
				if db == "" {
					db = "beads"
				}
				user := s.User
				if user == "" {
					user = "root"
				}
				doltLabel := fmt.Sprintf("dolt://%s/%s", s.Path, db)
				debug.Log("datasource: attempting Dolt connection to %s", doltLabel)
				dw, dwErr := datasource.NewDoltWatcher(sources[i], pollInterval)
				if dwErr != nil {
					debug.Log("datasource: Dolt watcher creation failed: %v", dwErr)
					doltFailure = &ui.DoltFailure{
						Server:   s.Path,
						Database: db,
						User:     user,
						Error:    dwErr.Error(),
					}
				} else {
					if err := dw.Start(); err != nil {
						debug.Log("datasource: Dolt watcher start failed: %v", err)
						dw.Stop()
						doltFailure = &ui.DoltFailure{
							Server:   s.Path,
							Database: db,
							User:     user,
							Error:    err.Error(),
						}
					} else {
						doltWatcher = dw
						doltSource = sources[i]
						detectedSourceType = datasource.SourceTypeDolt
						sourceInfo = doltLabel + " ✓"
						debug.Log("datasource: Dolt connected successfully to %s", doltLabel)
					}
				}
				break
			} else if s.Type == datasource.SourceTypeDoltEmbedded {
				// An embedded watcher holds nothing open, so only a bug fails it.
				if dw, dwErr := datasource.NewDoltWatcher(sources[i], pollInterval); dwErr == nil && dw.Start() == nil {
					doltWatcher = dw
					doltSource = sources[i]
					detectedSourceType = datasource.SourceTypeDoltEmbedded
					sourceInfo = s.EmbeddedLabel() + " ✓"
				}
				break
			} else if s.Type == datasource.SourceTypeSQLite {
				sourceInfo = fmt.Sprintf("sqlite %s", filepath.Base(s.Path))
			}
		}
	}
	if sourceInfo == "" {
		if beadsPath != "" {
			sourceInfo = fmt.Sprintf("jsonl %s", filepath.Base(beadsPath))
		} else {
			sourceInfo = "no source"
		}
	}
	debug.Log("datasource: selected source=%s type=%s", sourceInfo, detectedSourceType)
	return startupSource{sourceType: detectedSourceType, watcher: doltWatcher, source: doltSource, failure: doltFailure, info: sourceInfo}
}
