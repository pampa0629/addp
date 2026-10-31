package skp

// Nested container integrity only. XML/XSD/filter/image semantics, external
// references and complete model composition are deliberately not inferred.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
)

type vffClassificationPackageLimits struct {
	references                            vffClassificationLimits
	readBytes, expandedBytes, memberBytes int64
	packages, members                     int
}

type vffClassificationPackage struct {
	member      string
	sha256      [32]byte
	files       map[string][]byte
	directories []string
}

type vffClassificationPackageFacts struct {
	references *vffClassificationFacts
	packages   []vffClassificationPackage
}

func readVFFClassificationPackages(ctx context.Context, c *vffContainer, limits vffClassificationPackageLimits) (*vffClassificationPackageFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limits.readBytes <= 0 || limits.expandedBytes <= 0 || limits.memberBytes <= 0 || limits.packages <= 0 || limits.members <= 0 {
		return nil, fmt.Errorf("SKC containers require explicit positive cumulative budgets")
	}
	// Always derive references from the same current container. No old caller
	// receipt, package basename or content-equivalent member can substitute it.
	references, err := readVFFClassificationReferences(ctx, c, limits.references)
	if err != nil {
		return nil, err
	}
	result := &vffClassificationPackageFacts{references: references}
	seen := map[string]bool{}
	remainingReads, remainingExpanded, remainingMembers := limits.readBytes, limits.expandedBytes, limits.members
	for _, ref := range references.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[ref.member] {
			continue
		}
		if len(result.packages) >= limits.packages {
			return nil, fmt.Errorf("SKC cumulative package count exceeded")
		}
		seen[ref.member] = true
		raw := c.members[ref.member]
		if len(raw) < 22 || sha256.Sum256(raw) != ref.sha256 {
			return nil, fmt.Errorf("SKC member changed or cannot contain a ZIP directory")
		}
		source := &vffSource{ctx: ctx, input: bytes.NewReader(raw), size: int64(len(raw)), remaining: remainingReads}
		readLimits := vffReadLimits{memberBytes: limits.memberBytes, expandedBytes: remainingExpanded, members: remainingMembers}
		records, directory, err := readVFFDirectory(source, 0, readLimits, vffClassificationZIP)
		if err != nil {
			return nil, err
		}
		members, err := readVFFZIPMembers(source, 0, records, directory, vffClassificationZIP)
		if err != nil {
			return nil, err
		}
		item := vffClassificationPackage{member: ref.member, sha256: ref.sha256, files: map[string][]byte{}}
		for _, record := range records {
			remainingExpanded -= record.expanded // Directory already checked cumulative bounds.
			if record.directory {
				item.directories = append(item.directories, record.name)
			} else {
				item.files[record.name] = members[record.name]
			}
		}
		remainingReads, remainingMembers = source.remaining, remainingMembers-len(records)
		result.packages = append(result.packages, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
