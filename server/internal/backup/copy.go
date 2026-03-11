package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Destination string    `json:"destination"`
	FilesCopied int       `json:"filesCopied"`
	BytesCopied int64     `json:"bytesCopied"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
}

func Run(ctx context.Context, sourceDir string, targetDir string) (Result, error) {
	startedAt := time.Now().UTC()

	sourceAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve source path: %w", err)
	}

	targetAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve target path: %w", err)
	}

	if strings.EqualFold(sourceAbs, targetAbs) {
		return Result{}, errors.New("source and target directories must be different")
	}

	if isSubPath(sourceAbs, targetAbs) {
		return Result{}, errors.New("target directory cannot be inside source directory")
	}

	sourceInfo, err := os.Stat(sourceAbs)
	if err != nil {
		return Result{}, fmt.Errorf("source directory does not exist: %w", err)
	}
	if !sourceInfo.IsDir() {
		return Result{}, errors.New("source path must be a directory")
	}

	if err := os.MkdirAll(targetAbs, 0o755); err != nil {
		return Result{}, fmt.Errorf("create target directory: %w", err)
	}

	runDir := filepath.Join(targetAbs, "snapshot-"+startedAt.Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	result := Result{
		Destination: runDir,
		StartedAt:   startedAt,
	}

	err = filepath.WalkDir(sourceAbs, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == sourceAbs {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relativePath, err := filepath.Rel(sourceAbs, currentPath)
		if err != nil {
			return fmt.Errorf("build relative path: %w", err)
		}
		destinationPath := filepath.Join(runDir, relativePath)

		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		bytesCopied, err := copyFile(currentPath, destinationPath, info.Mode())
		if err != nil {
			return err
		}

		result.FilesCopied++
		result.BytesCopied += bytesCopied
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	result.FinishedAt = time.Now().UTC()
	return result, nil
}

func copyFile(sourcePath string, destinationPath string, mode fs.FileMode) (int64, error) {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return 0, err
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return 0, err
	}
	defer destinationFile.Close()

	return io.Copy(destinationFile, sourceFile)
}

func isSubPath(basePath string, candidatePath string) bool {
	baseWithSeparator := basePath + string(filepath.Separator)
	candidateWithSeparator := candidatePath + string(filepath.Separator)
	return strings.HasPrefix(candidateWithSeparator, baseWithSeparator)
}
