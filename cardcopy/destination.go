package cardcopy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// destinationLocation remembers the identity of the nearest existing directory
// at planning time without keeping file descriptors alive in an inspectable Plan.
// If the base doesn't exist yet, execution creates it beneath this trusted anchor.
type destinationLocation struct {
	basePath     string
	ancestorPath string
	ancestorInfo os.FileInfo
	sourceInfo   os.FileInfo
}

// Check identities, not just spellings: on common macOS volumes, CARD and
// Card name the same directory. A missing base still has an existing anchor.
func (d destinationLocation) checkOutsideCard() error {
	path, info := d.ancestorPath, d.ancestorInfo
	for {
		if os.SameFile(info, d.sourceInfo) {
			return fmt.Errorf("destination %s is inside source card", d.basePath)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		var err error
		info, err = os.Stat(parent)
		if err != nil {
			return fmt.Errorf("inspecting destination ancestor %s: %w", parent, err)
		}
		path = parent
	}
}

func planDestination(base string, sourceInfo os.FileInfo) (destinationLocation, *os.Root, error) {
	for path := base; ; path = filepath.Dir(path) {
		root, err := os.OpenRoot(path)
		if os.IsNotExist(err) && filepath.Dir(path) != path {
			continue
		}
		if err != nil {
			return destinationLocation{}, nil, fmt.Errorf("opening destination: %w", err)
		}
		info, err := root.Stat(".")
		if err != nil {
			root.Close()
			return destinationLocation{}, nil, fmt.Errorf("inspecting destination: %w", err)
		}
		location := destinationLocation{basePath: base, ancestorPath: path, ancestorInfo: info, sourceInfo: sourceInfo}
		if err := location.checkOutsideCard(); err != nil {
			root.Close()
			return destinationLocation{}, nil, err
		}
		if path == base {
			return location, root, nil
		}
		root.Close()
		return location, nil, nil
	}
}

func (d destinationLocation) open() (*os.Root, error) {
	if d.ancestorInfo == nil {
		return nil, fmt.Errorf("destination must be resolved by PlanCopy before execution")
	}
	root, err := os.OpenRoot(d.ancestorPath)
	if err != nil {
		return nil, fmt.Errorf("opening planned destination: %w", err)
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(d.ancestorInfo, info) {
		root.Close()
		return nil, fmt.Errorf("destination directory changed after planning: %s", d.ancestorPath)
	}
	if err := d.checkOutsideCard(); err != nil {
		root.Close()
		return nil, err
	}
	if d.basePath == d.ancestorPath {
		return root, nil
	}
	rel, err := filepath.Rel(d.ancestorPath, d.basePath)
	if err != nil || !filepath.IsLocal(rel) {
		root.Close()
		return nil, fmt.Errorf("invalid planned destination: %s", d.basePath)
	}
	// Open one component at a time, verifying its identity before making any
	// changes beneath it. An initially absent destination must not become a
	// symlink to a different tree between planning and execution.
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		next, err := openDestinationChild(root, component, d.sourceInfo)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func openDestinationChild(parent *os.Root, name string, sourceInfo os.FileInfo) (*os.Root, error) {
	if err := parent.Mkdir(name, 0755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("creating destination directory: %w", err)
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspecting destination directory: %w", err)
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("destination component is not a directory: %s", name)
	}
	if os.SameFile(sourceInfo, before) {
		return nil, fmt.Errorf("destination component %s is the source card", name)
	}
	next, err := parent.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("opening destination directory: %w", err)
	}
	after, err := next.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		next.Close()
		return nil, fmt.Errorf("destination directory changed while opening: %s", name)
	}
	return next, nil
}
