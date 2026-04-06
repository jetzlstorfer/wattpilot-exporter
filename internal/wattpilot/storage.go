package wattpilot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// DataStore abstracts the underlying storage backend (local filesystem or Azure Blob Storage).
type DataStore interface {
	// Read returns the contents of the named file/blob.
	Read(ctx context.Context, name string) ([]byte, error)
	// Write persists data under the given name, creating or replacing it atomically.
	Write(ctx context.Context, name string, data []byte) error
	// ModTime returns the last-modified time of the named file/blob.
	// Returns os.ErrNotExist if the file/blob does not exist.
	ModTime(ctx context.Context, name string) (time.Time, error)
}

// globalStore is the active DataStore, initialised by InitStore.
var globalStore DataStore = LocalStore{}

// InitStore selects the storage backend based on environment variables.
// If AZURE_STORAGE_ACCOUNT_NAME is set, Azure Blob Storage is used with
// DefaultAzureCredential (managed identity in Azure, az-login locally).
// Otherwise, the local filesystem is used.
func InitStore(ctx context.Context) {
	accountName := os.Getenv("AZURE_STORAGE_ACCOUNT_NAME")
	if accountName == "" {
		slog.InfoContext(ctx, "AZURE_STORAGE_ACCOUNT_NAME not set – using local filesystem storage")
		globalStore = LocalStore{}
		return
	}

	containerName := os.Getenv("AZURE_STORAGE_CONTAINER_NAME")
	if containerName == "" {
		containerName = "wattpilot-data"
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create Azure credential – falling back to local storage", "error", err)
		globalStore = LocalStore{}
		return
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create Azure Blob client – falling back to local storage", "error", err)
		globalStore = LocalStore{}
		return
	}

	containerClient := client.ServiceClient().NewContainerClient(containerName)

	slog.InfoContext(ctx, "Using Azure Blob Storage",
		"account", accountName,
		"container", containerName,
	)
	globalStore = &AzureBlobStore{containerClient: containerClient}
}

// ---------------------------------------------------------------------------
// LocalStore – local filesystem implementation
// ---------------------------------------------------------------------------

// LocalStore implements DataStore using the local filesystem.
type LocalStore struct{}

func (LocalStore) Read(_ context.Context, name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (LocalStore) Write(_ context.Context, name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %v", name, err)
	}

	// Write to a temp file first, then rename atomically.
	tmpFile, err := os.CreateTemp(filepath.Dir(name), ".tmp-wattpilot-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	tmpName := tmpFile.Name()

	_, writeErr := tmpFile.Write(data)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to write data: %v", writeErr)
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %v", closeErr)
	}

	if err := os.Rename(tmpName, name); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file to %s: %v", name, err)
	}
	return nil
}

func (LocalStore) ModTime(_ context.Context, name string) (time.Time, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// ---------------------------------------------------------------------------
// AzureBlobStore – Azure Blob Storage implementation
// ---------------------------------------------------------------------------

// AzureBlobStore implements DataStore using Azure Blob Storage.
type AzureBlobStore struct {
	containerClient *container.Client
}

func (s *AzureBlobStore) Read(ctx context.Context, name string) ([]byte, error) {
	resp, err := s.containerClient.NewBlockBlobClient(name).DownloadStream(ctx, nil)
	if err != nil {
		if isBlobNotFound(err) {
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
		}
		return nil, fmt.Errorf("failed to download blob %s: %w", name, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (s *AzureBlobStore) Write(ctx context.Context, name string, data []byte) error {
	_, err := s.containerClient.NewBlockBlobClient(name).UploadStream(ctx, bytes.NewReader(data), nil)
	if err != nil {
		return fmt.Errorf("failed to upload blob %s: %w", name, err)
	}
	return nil
}

func (s *AzureBlobStore) ModTime(ctx context.Context, name string) (time.Time, error) {
	resp, err := s.containerClient.NewBlockBlobClient(name).GetProperties(ctx, nil)
	if err != nil {
		if isBlobNotFound(err) {
			return time.Time{}, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
		}
		return time.Time{}, fmt.Errorf("failed to get blob properties for %s: %w", name, err)
	}
	if resp.LastModified == nil {
		return time.Time{}, fmt.Errorf("blob %s has no last-modified time", name)
	}
	return *resp.LastModified, nil
}

// isBlobNotFound reports whether the Azure SDK error represents a 404.
func isBlobNotFound(err error) bool {
	var respErr interface{ StatusCode() int }
	if errors.As(err, &respErr) {
		return respErr.StatusCode() == 404
	}
	return false
}
