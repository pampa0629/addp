package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/addp/common/authorization"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "authorization catalog command failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("authorization-manifest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "validate repository manifests and write the canonical report to stdout")
	generateOwnerConstants := flags.Bool("generate-owner-constants", false, "generate owner-local Permission constants")
	checkOwnerConstants := flags.Bool("check-owner-constants", false, "verify owner-local Permission constants are current")
	coverageReport := flags.Bool("coverage-report", false, "write the OpenAPI and Tool authorization coverage report")
	generateSQLSeed := flags.Bool("generate-sql-seed", false, "generate the first IAM catalog seed migration")
	checkSQLSeed := flags.Bool("check-sql-seed", false, "verify the first IAM catalog seed migration is current")
	repositoryRoot := flags.String("repository-root", "", "explicit ADDP repository root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	modeCount := 0
	for _, enabled := range []bool{
		*check,
		*generateOwnerConstants,
		*checkOwnerConstants,
		*coverageReport,
		*generateSQLSeed,
		*checkSQLSeed,
	} {
		if enabled {
			modeCount++
		}
	}
	if modeCount != 1 {
		return fmt.Errorf("exactly one manifest command mode is required")
	}
	if *repositoryRoot == "" {
		return fmt.Errorf("--repository-root is required")
	}

	report, err := authorization.LoadRepositoryAuthorizationCatalog(*repositoryRoot)
	if err != nil {
		return err
	}
	if *check {
		data, err := authorization.MarshalAuthorizationCatalogReport(report)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write authorization catalog report: %w", err)
		}
		return nil
	}
	if *coverageReport {
		coverage, err := authorization.BuildRepositoryAuthorizationCoverageReport(*repositoryRoot, report)
		if err != nil {
			return err
		}
		data, err := authorization.MarshalAuthorizationCoverageReport(coverage)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write authorization coverage report: %w", err)
		}
		return nil
	}
	if *generateSQLSeed || *checkSQLSeed {
		data, err := authorization.GenerateIAMCatalogSeedSQL(report)
		if err != nil {
			return err
		}
		if *generateSQLSeed {
			if err := authorization.WriteGeneratedIAMCatalogSeed(*repositoryRoot, data); err != nil {
				return err
			}
		} else if err := authorization.CheckGeneratedIAMCatalogSeed(*repositoryRoot, data); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "{\"path\":%q,\"byte_count\":%d}\n", authorization.IAMCatalogSeedRelativePath, len(data)); err != nil {
			return fmt.Errorf("write generated IAM catalog seed report: %w", err)
		}
		return nil
	}

	files, err := authorization.GenerateOwnerPermissionConstants(report)
	if err != nil {
		return err
	}
	if *generateOwnerConstants {
		if err := authorization.WriteGeneratedOwnerPermissionConstants(*repositoryRoot, files); err != nil {
			return err
		}
	} else if err := authorization.CheckGeneratedOwnerPermissionConstants(*repositoryRoot, files); err != nil {
		return err
	}

	data, err := authorization.MarshalGeneratedOwnerConstantsReport(authorization.BuildGeneratedOwnerConstantsReport(files))
	if err != nil {
		return err
	}
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write generated owner constants report: %w", err)
	}
	return nil
}
