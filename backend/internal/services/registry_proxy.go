package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ProxyHelper handles fetching manifests and blobs from upstream (Docker Hub)
type ProxyHelper struct {
	client *http.Client
}

func NewProxyHelper() *ProxyHelper {
	return &ProxyHelper{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// getDockerHubToken gets an anonymous pull token for a specific repository
func (p *ProxyHelper) getDockerHubToken(repository string) (string, error) {
	url := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", repository)
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get token, status %d", resp.StatusCode)
	}

	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.Token, nil
}

// FetchManifest fetches a manifest from Docker Hub and saves it locally
func (p *ProxyHelper) FetchManifest(projectID, repoName, reference, destPath string, r *http.Request) (string, error) {
	repository := projectID + "/" + repoName
	if projectID == "library" {
		repository = "library/" + repoName // e.g. library/alpine
	}

	token, err := p.getDockerHubToken(repository)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://registry-1.docker.io/v2/%s/manifests/%s", repository, reference)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// Forward Accept headers if present, else provide common ones
	accept := r.Header.Get("Accept")
	if accept == "" {
		accept = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json"
	}
	req.Header.Set("Accept", accept)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	// Save to disk
	os.MkdirAll(filepath.Dir(destPath), 0755)
	f, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	io.Copy(f, resp.Body)

	contentType := resp.Header.Get("Content-Type")
	// Save content type for future local serving
	os.WriteFile(destPath+".content-type", []byte(contentType), 0644)

	return contentType, nil
}

// FetchBlob fetches a blob concurrently using Range requests
func (p *ProxyHelper) FetchBlob(projectID, repoName, digest, destPath string) error {
	repository := projectID + "/" + repoName
	if projectID == "library" {
		repository = "library/" + repoName
	}

	token, err := p.getDockerHubToken(repository)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://registry-1.docker.io/v2/%s/blobs/%s", repository, digest)

	// First, get the content length
	req, _ := http.NewRequest("HEAD", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream blob HEAD returned %d", resp.StatusCode)
	}

	contentLength := resp.Header.Get("Content-Length")
	size, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil || size == 0 {
		// Fallback to single stream download
		return p.downloadSingle(url, token, destPath)
	}

	// Multi-worker concurrent download
	workers := 4
	if size < 5*1024*1024 {
		workers = 1 // single worker for < 5MB
	}

	return p.downloadConcurrent(url, token, destPath, size, workers)
}

func (p *ProxyHelper) downloadSingle(url, token, destPath string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	os.MkdirAll(filepath.Dir(destPath), 0755)
	f, err := os.Create(destPath + ".tmp")
	if err != nil {
		return err
	}

	io.Copy(f, resp.Body)
	f.Close()
	return os.Rename(destPath+".tmp", destPath)
}

func (p *ProxyHelper) downloadConcurrent(url, token, destPath string, size int64, workers int) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)

	// Pre-allocate file
	f, err := os.Create(destPath + ".tmp")
	if err != nil {
		return err
	}
	f.Truncate(size)
	f.Close()

	chunkSize := size / int64(workers)
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == workers-1 {
			end = size - 1 // Last chunk gets the remainder
		}

		wg.Add(1)
		go func(workerID int, start, end int64) {
			defer wg.Done()

			// Use a separate client/request for each chunk
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			client := &http.Client{Timeout: 5 * time.Minute}
			resp, err := client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("chunk download failed with status %d", resp.StatusCode)
				return
			}

			// Write directly to the pre-allocated file segment
			outFile, err := os.OpenFile(destPath+".tmp", os.O_WRONLY, 0644)
			if err != nil {
				errs <- err
				return
			}
			defer outFile.Close()
			outFile.Seek(start, 0)

			_, err = io.Copy(outFile, resp.Body)
			if err != nil {
				errs <- err
				return
			}
		}(i, start, end)
	}

	wg.Wait()
	close(errs)

	if len(errs) > 0 {
		os.Remove(destPath + ".tmp")
		return <-errs // Return first error
	}

	// Rename temp file to final destination once complete
	return os.Rename(destPath+".tmp", destPath)
}
