package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Progress struct {
	Phase          string
	CurrentPath    string
	TotalFiles     int
	ProcessedFiles int
	FilesCopied    int
	FilesDeleted   int
	BytesCopied    int64
	Percent        float64
}

type ProgressFunc func(progress Progress)

type Result struct {
	Destination  string    `json:"destination"`
	FilesCopied  int       `json:"filesCopied"`
	FilesDeleted int       `json:"filesDeleted"`
	BytesCopied  int64     `json:"bytesCopied"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
}

type sourceFile struct {
	relativePath string
	absolutePath string
	size         int64
	mode         fs.FileMode
	modTime      time.Time
}

func Run(ctx context.Context, sourceDir string, targetDir string, onProgress ProgressFunc) (Result, error) {
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

	sourceName := filepath.Base(filepath.Clean(sourceAbs))
	if sourceName == string(filepath.Separator) || sourceName == "." {
		return Result{}, errors.New("source directory name is invalid")
	}

	destinationRoot := filepath.Join(targetAbs, sourceName)

	if samePath(sourceAbs, destinationRoot) {
		return Result{}, errors.New("target directory cannot mirror to the same source path")
	}

	if isSubPath(sourceAbs, destinationRoot) {
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

	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("create destination directory: %w", err)
	}

	result := Result{
		Destination: destinationRoot,
		StartedAt:   startedAt,
	}

	sourceFiles := make([]sourceFile, 0, 256)
	sourceEntries := make(map[string]struct{}, 512)

	report := func(progress Progress) {
		if onProgress == nil {
			return
		}
		onProgress(progress)
	}

	report(Progress{
		Phase:       "scanning",
		CurrentPath: sourceAbs,
	})

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
		normalizedKey := normalizeRelativePath(relativePath)
		sourceEntries[normalizedKey] = struct{}{}

		if entry.IsDir() {
			destinationPath := filepath.Join(destinationRoot, relativePath)
			return ensureDir(destinationPath)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		sourceFiles = append(sourceFiles, sourceFile{
			relativePath: relativePath,
			absolutePath: currentPath,
			size:         info.Size(),
			mode:         info.Mode(),
			modTime:      info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	totalFiles := len(sourceFiles)
	if totalFiles == 0 {
		report(Progress{
			Phase:       "deleting",
			CurrentPath: destinationRoot,
			TotalFiles:  0,
			Percent:     100,
		})
	} else {
		report(Progress{
			Phase:      "copying",
			TotalFiles: totalFiles,
			Percent:    0,
		})
	}

	for index, file := range sourceFiles {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}

		destinationPath := filepath.Join(destinationRoot, file.relativePath)
		copied, bytesCopied, err := syncFile(file, destinationPath)
		if err != nil {
			return Result{}, err
		}

		if copied {
			result.FilesCopied++
			result.BytesCopied += bytesCopied
		}

		processed := index + 1
		report(Progress{
			Phase:          "copying",
			CurrentPath:    file.relativePath,
			TotalFiles:     totalFiles,
			ProcessedFiles: processed,
			FilesCopied:    result.FilesCopied,
			FilesDeleted:   result.FilesDeleted,
			BytesCopied:    result.BytesCopied,
			Percent:        percent(processed, totalFiles),
		})
	}

	report(Progress{
		Phase:          "deleting",
		CurrentPath:    destinationRoot,
		TotalFiles:     totalFiles,
		ProcessedFiles: totalFiles,
		FilesCopied:    result.FilesCopied,
		FilesDeleted:   result.FilesDeleted,
		BytesCopied:    result.BytesCopied,
		Percent:        100,
	})

	err = filepath.WalkDir(destinationRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == destinationRoot {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relativePath, err := filepath.Rel(destinationRoot, currentPath)
		if err != nil {
			return err
		}

		if _, exists := sourceEntries[normalizeRelativePath(relativePath)]; exists {
			return nil
		}

		if entry.IsDir() {
			if err := os.RemoveAll(currentPath); err != nil {
				return err
			}
			result.FilesDeleted++
			return filepath.SkipDir
		}

		if err := os.Remove(currentPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		result.FilesDeleted++
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	result.FinishedAt = time.Now().UTC()
	report(Progress{
		Phase:          "done",
		CurrentPath:    destinationRoot,
		TotalFiles:     totalFiles,
		ProcessedFiles: totalFiles,
		FilesCopied:    result.FilesCopied,
		FilesDeleted:   result.FilesDeleted,
		BytesCopied:    result.BytesCopied,
		Percent:        100,
	})
	return result, nil
}

func syncFile(source sourceFile, destinationPath string) (bool, int64, error) {
	shouldCopy := false

	destinationInfo, err := os.Stat(destinationPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, 0, err
		}
		shouldCopy = true
	} else {
		if !destinationInfo.Mode().IsRegular() {
			if removeErr := os.RemoveAll(destinationPath); removeErr != nil {
				return false, 0, removeErr
			}
			shouldCopy = true
		} else if destinationInfo.Size() != source.size {
			shouldCopy = true
		} else {
			modDiff := source.modTime.Sub(destinationInfo.ModTime())
			if modDiff < 0 {
				modDiff = -modDiff
			}
			if modDiff > time.Second {
				shouldCopy = true
			}
		}
	}

	if !shouldCopy {
		return false, 0, nil
	}

	bytesCopied, err := copyFile(source.absolutePath, destinationPath, source.mode)
	if err != nil {
		return false, 0, err
	}

	_ = os.Chtimes(destinationPath, time.Now(), source.modTime)
	return true, bytesCopied, nil
}

func copyFile(sourcePath string, destinationPath string, mode fs.FileMode) (int64, error) {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	if err := ensureDir(filepath.Dir(destinationPath)); err != nil {
		return 0, err
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return 0, err
	}
	defer destinationFile.Close()

	return io.Copy(destinationFile, sourceFile)
}

func percent(processed int, total int) float64 {
	if total <= 0 {
		return 100
	}
	return float64(processed) * 100 / float64(total)
}

func isSubPath(basePath string, candidatePath string) bool {
	baseWithSeparator := normalizeForCompare(filepath.Clean(basePath)) + string(filepath.Separator)
	candidateWithSeparator := normalizeForCompare(filepath.Clean(candidatePath)) + string(filepath.Separator)
	return strings.HasPrefix(candidateWithSeparator, baseWithSeparator)
}

func samePath(pathA string, pathB string) bool {
	return normalizeForCompare(pathA) == normalizeForCompare(pathB)
}

func normalizeRelativePath(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func normalizeForCompare(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func ensureDir(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return removeErr
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return os.MkdirAll(path, 0o755)
}
