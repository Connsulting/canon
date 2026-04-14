package canon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type layoutRole string

const (
	layoutRoleCore    layoutRole = "core"
	layoutRoleSupport layoutRole = "support"
)

type layoutPath struct {
	Rel  string
	Role layoutRole
}

var requiredLayoutPaths = []layoutPath{
	{Rel: ".canon/specs", Role: layoutRoleCore},
	{Rel: ".canon/ledger", Role: layoutRoleCore},
	{Rel: ".canon/sources", Role: layoutRoleCore},
	{Rel: ".canon/conflict-reports", Role: layoutRoleSupport},
	{Rel: ".canon/archive/specs", Role: layoutRoleSupport},
	{Rel: ".canon/archive/sources", Role: layoutRoleSupport},
	{Rel: "state/interactions", Role: layoutRoleSupport},
}

type LayoutHealth string

const (
	LayoutHealthy    LayoutHealth = "healthy"
	LayoutRepairable LayoutHealth = "repairable"
	LayoutInvalid    LayoutHealth = "invalid"
)

type LayoutProblemKind string

const (
	LayoutProblemMissingCore  LayoutProblemKind = "missing_core"
	LayoutProblemNotDirectory LayoutProblemKind = "not_directory"
	LayoutProblemInaccessible LayoutProblemKind = "inaccessible"
	LayoutProblemInvalidData  LayoutProblemKind = "invalid_data"
)

type LayoutProblem struct {
	Path string
	Kind LayoutProblemKind
	Err  error
}

type LayoutReport struct {
	Health         LayoutHealth
	MissingSupport []string
	Problems       []LayoutProblem
}

type LayoutError struct {
	Report LayoutReport
}

func (e LayoutError) Error() string {
	return e.Report.ErrorMessage()
}

func EnsureLayout(root string, createMissing bool) error {
	if createMissing {
		for _, path := range requiredLayoutPaths {
			abs := filepath.Join(root, path.Rel)
			st, err := os.Stat(abs)
			if err == nil {
				if !st.IsDir() {
					return fmt.Errorf("required path is not a directory: %s", path.Rel)
				}
				continue
			}
			if !os.IsNotExist(err) {
				return err
			}
			if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
				return mkErr
			}
		}
		return nil
	}

	report := CheckLayout(root)
	if report.Health != LayoutHealthy {
		return LayoutError{Report: report}
	}
	return nil
}

func CheckLayout(root string) LayoutReport {
	report := LayoutReport{Health: LayoutHealthy}
	for _, path := range requiredLayoutPaths {
		abs := filepath.Join(root, path.Rel)
		st, err := os.Stat(abs)
		if err == nil {
			if !st.IsDir() {
				report.Problems = append(report.Problems, LayoutProblem{Path: path.Rel, Kind: LayoutProblemNotDirectory})
			}
			continue
		}
		if !os.IsNotExist(err) {
			report.Problems = append(report.Problems, LayoutProblem{Path: path.Rel, Kind: LayoutProblemInaccessible, Err: err})
			continue
		}
		if path.Role == layoutRoleCore {
			report.Problems = append(report.Problems, LayoutProblem{Path: path.Rel, Kind: LayoutProblemMissingCore})
			continue
		}
		report.MissingSupport = append(report.MissingSupport, path.Rel)
	}

	if len(report.Problems) > 0 {
		report.Health = LayoutInvalid
	} else if len(report.MissingSupport) > 0 {
		report.Health = LayoutRepairable
	}
	return report
}

func (r LayoutReport) ErrorMessage() string {
	switch r.Health {
	case LayoutRepairable:
		return fmt.Sprintf("repairable repository layout: missing support directories: %s", strings.Join(r.MissingSupport, ", "))
	case LayoutInvalid:
		parts := make([]string, 0, len(r.Problems))
		for _, problem := range r.Problems {
			switch problem.Kind {
			case LayoutProblemMissingCore:
				parts = append(parts, fmt.Sprintf("missing required core directory: %s", problem.Path))
			case LayoutProblemNotDirectory:
				parts = append(parts, fmt.Sprintf("required path is not a directory: %s", problem.Path))
			case LayoutProblemInaccessible:
				if problem.Err != nil {
					parts = append(parts, fmt.Sprintf("required path is inaccessible: %s: %v", problem.Path, problem.Err))
				} else {
					parts = append(parts, fmt.Sprintf("required path is inaccessible: %s", problem.Path))
				}
			case LayoutProblemInvalidData:
				if problem.Err != nil {
					parts = append(parts, fmt.Sprintf("repository data is invalid: %s: %v", problem.Path, problem.Err))
				} else {
					parts = append(parts, fmt.Sprintf("repository data is invalid: %s", problem.Path))
				}
			}
		}
		return "invalid repository layout: " + strings.Join(parts, "; ")
	default:
		return "repository layout is healthy"
	}
}
