// Package b2 implements just enough of the Backblaze B2 native API to
// authorize, create/configure buckets and keys, and upload files (including
// the large-file multipart flow for backups over ~100MB) — all with Go's
// standard library only. This exists to remove the rclone and b2-CLI
// dependencies entirely, so the backup tool has no external programs to
// install on the client's server besides PostgreSQL's own client tools.
package b2

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const authorizeURL = "https://api.backblazeb2.com/b2api/v2/b2_authorize_account"

// LargeFilePartSize is the size of each part in a large-file multipart
// upload. B2's minimum is 5MB; we use 100MB for a good balance between
// request count and retry granularity on flaky connections.
const LargeFilePartSize = 100 * 1024 * 1024

// LargeFileThreshold: files at or above this size use the multipart large
// file API instead of a single-request upload, so a transient network
// failure only costs one part's retry, not the whole backup.
const LargeFileThreshold = 190 * 1024 * 1024

type Client struct {
	httpClient    *http.Client
	accountID     string
	authToken     string
	apiURL        string
	downloadURL   string
	authorizedAt  time.Time
	keyID, appKey string
}

func New(keyID, applicationKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 2 * time.Hour},
		keyID:      keyID,
		appKey:     applicationKey,
	}
}

// authorizeResponse covers both shapes Backblaze has returned from
// /b2api/v2/b2_authorize_account in the wild: the flat one (apiUrl and
// downloadUrl at the top level — what a full-capability account/master
// key gets) and the newer nested one (apiInfo.storageApi.apiUrl — seen
// with more narrowly-scoped keys). Real-world testing against a live
// account showed the flat shape, contradicting the nested shape shown
// in some of Backblaze's own docs pages, so both are parsed and the
// nested one wins only if present.
type authorizeResponse struct {
	AccountID string `json:"accountId"`
	AuthToken string `json:"authorizationToken"`
	Allowed   struct {
		Capabilities []string `json:"capabilities"`
		BucketID     *string  `json:"bucketId"`
		BucketName   *string  `json:"bucketName"`
	} `json:"allowed"`

	// Flat shape.
	FlatAPIURL      string `json:"apiUrl"`
	FlatDownloadURL string `json:"downloadUrl"`

	// Nested shape.
	APIInfo struct {
		StorageAPI struct {
			APIURL      string `json:"apiUrl"`
			DownloadURL string `json:"downloadUrl"`
		} `json:"storageApi"`
	} `json:"apiInfo"`
}

func (ar authorizeResponse) resolvedAPIURL() string {
	if ar.APIInfo.StorageAPI.APIURL != "" {
		return ar.APIInfo.StorageAPI.APIURL
	}
	return ar.FlatAPIURL
}

func (ar authorizeResponse) resolvedDownloadURL() string {
	if ar.APIInfo.StorageAPI.DownloadURL != "" {
		return ar.APIInfo.StorageAPI.DownloadURL
	}
	return ar.FlatDownloadURL
}

type apiError struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("b2 api error (status %d, code %s): %s", e.Status, e.Code, e.Message)
}

// Authorize must be called once before any other operation. Tokens are
// valid for 24h; callers running long sessions should re-Authorize if a
// call fails with an expired-auth error.
func (c *Client) Authorize() error {
	req, err := http.NewRequest("GET", authorizeURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.keyID, c.appKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar na API do B2: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp.StatusCode, body)
	}

	var ar authorizeResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return fmt.Errorf("resposta inesperada do b2_authorize_account: %w", err)
	}

	apiURL := ar.resolvedAPIURL()
	if apiURL == "" {
		// A 200 response with no apiUrl in either shape means the key
		// authenticated fine but has no capability that touches
		// buckets/files. Report that clearly instead of letting the
		// caller hit a confusing "unsupported protocol scheme" once it
		// tries to build a request against an empty API URL.
		return fmt.Errorf(
			"a chave autenticou, mas não trouxe apiUrl em nenhum formato de resposta conhecido. "+
				"Capabilities atuais da chave: %v. Essa chave precisa de capabilities como listBuckets, writeBuckets, listKeys e writeKeys — "+
				"confira em backblaze.com > Application Keys se é essa a chave certa, ou crie uma nova com essas permissões",
			ar.Allowed.Capabilities,
		)
	}

	c.accountID = ar.AccountID
	c.authToken = ar.AuthToken
	c.apiURL = apiURL
	c.downloadURL = ar.resolvedDownloadURL()
	c.authorizedAt = time.Now()
	return nil
}

func parseAPIError(status int, body []byte) error {
	var ae apiError
	if err := json.Unmarshal(body, &ae); err == nil && ae.Message != "" {
		ae.Status = status
		return &ae
	}
	return fmt.Errorf("b2 api error (status %d): %s", status, string(body))
}

func (c *Client) ensureAuthorized() error {
	if c.authToken == "" || time.Since(c.authorizedAt) > 23*time.Hour {
		return c.Authorize()
	}
	return nil
}

func (c *Client) apiPOST(endpoint string, reqBody, respBody interface{}) error {
	if err := c.ensureAuthorized(); err != nil {
		return err
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.apiURL+"/b2api/v2/"+endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha na chamada %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp.StatusCode, body)
	}
	if respBody != nil {
		if err := json.Unmarshal(body, respBody); err != nil {
			return fmt.Errorf("resposta inesperada de %s: %w", endpoint, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// Buckets
// ---------------------------------------------------------------------

type LifecycleRule struct {
	FileNamePrefix            string `json:"fileNamePrefix"`
	DaysFromUploadingToHiding *int   `json:"daysFromUploadingToHiding,omitempty"`
	DaysFromHidingToDeleting  *int   `json:"daysFromHidingToDeleting,omitempty"`
}

type Bucket struct {
	BucketID       string          `json:"bucketId"`
	BucketName     string          `json:"bucketName"`
	BucketType     string          `json:"bucketType"`
	LifecycleRules []LifecycleRule `json:"lifecycleRules"`
}

func (c *Client) FindBucketByName(name string) (*Bucket, error) {
	var resp struct {
		Buckets []Bucket `json:"buckets"`
	}
	req := map[string]interface{}{
		"accountId":  c.accountID,
		"bucketName": name,
	}
	if err := c.apiPOST("b2_list_buckets", req, &resp); err != nil {
		return nil, err
	}
	if len(resp.Buckets) == 0 {
		return nil, nil
	}
	return &resp.Buckets[0], nil
}

func (c *Client) CreateBucket(name string, rules []LifecycleRule) (*Bucket, error) {
	var bucket Bucket
	req := map[string]interface{}{
		"accountId":      c.accountID,
		"bucketName":     name,
		"bucketType":     "allPrivate",
		"lifecycleRules": rules,
	}
	if err := c.apiPOST("b2_create_bucket", req, &bucket); err != nil {
		return nil, err
	}
	return &bucket, nil
}

// UpdateBucketLifecycle replaces the bucket's lifecycle rules wholesale.
// Callers should merge with any existing rules first if they want to
// preserve rules set outside this tool.
func (c *Client) UpdateBucketLifecycle(bucketID string, rules []LifecycleRule) error {
	req := map[string]interface{}{
		"accountId":      c.accountID,
		"bucketId":       bucketID,
		"lifecycleRules": rules,
	}
	return c.apiPOST("b2_update_bucket", req, nil)
}

// ---------------------------------------------------------------------
// Application keys
// ---------------------------------------------------------------------

type ApplicationKey struct {
	ApplicationKeyID string `json:"applicationKeyId"`
	ApplicationKey   string `json:"applicationKey"`
}

// CreateRestrictedKey creates a key limited to one bucket and one file
// name prefix, with just the capabilities the backup tool needs day to
// day (never full account access).
func (c *Client) CreateRestrictedKey(bucketID, keyName, prefix string) (*ApplicationKey, error) {
	var key ApplicationKey
	req := map[string]interface{}{
		"accountId": c.accountID,
		"capabilities": []string{
			"listFiles", "readFiles", "writeFiles", "deleteFiles", "listBuckets",
		},
		"keyName":    keyName,
		"bucketId":   bucketID,
		"namePrefix": prefix,
	}
	if err := c.apiPOST("b2_create_key", req, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// ---------------------------------------------------------------------
// Connectivity test
// ---------------------------------------------------------------------

// ListFileNames is used by the "teste-b2" check: it proves the key
// authenticates and can see into the bucket, without uploading anything.
func (c *Client) ListFileNames(bucketID string, maxCount int) error {
	req := map[string]interface{}{
		"bucketId":     bucketID,
		"maxFileCount": maxCount,
	}
	return c.apiPOST("b2_list_file_names", req, nil)
}

// ---------------------------------------------------------------------
// Simple (single-request) upload — used for small files like the
// verification result reports.
// ---------------------------------------------------------------------

type uploadURLResponse struct {
	UploadURL string `json:"uploadUrl"`
	AuthToken string `json:"authorizationToken"`
}

func (c *Client) getUploadURL(bucketID string) (*uploadURLResponse, error) {
	var resp uploadURLResponse
	req := map[string]interface{}{"bucketId": bucketID}
	if err := c.apiPOST("b2_get_upload_url", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadSmallFile uploads data in one HTTP request. Suitable for files
// comfortably under LargeFileThreshold.
func (c *Client) UploadSmallFile(bucketID, fileName string, data []byte) error {
	uploadInfo, err := c.getUploadURL(bucketID)
	if err != nil {
		return fmt.Errorf("falha ao obter URL de upload: %w", err)
	}

	sum := sha1.Sum(data)
	sha1Hex := hex.EncodeToString(sum[:])

	req, err := http.NewRequest("POST", uploadInfo.UploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", uploadInfo.AuthToken)
	req.Header.Set("X-Bz-File-Name", url.PathEscape(fileName))
	req.Header.Set("Content-Type", "b2/x-auto")
	req.Header.Set("X-Bz-Content-Sha1", sha1Hex)
	req.ContentLength = int64(len(data))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha no upload: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp.StatusCode, body)
	}
	return nil
}

// ---------------------------------------------------------------------
// Large file (multipart) upload — used for backup archives, which
// routinely run 300-500MB+. Splitting into parts means a dropped
// connection only costs a retry of the current part, not the whole file.
// ---------------------------------------------------------------------

type startLargeFileResponse struct {
	FileID string `json:"fileId"`
}

type getPartUploadURLResponse struct {
	UploadURL string `json:"uploadUrl"`
	AuthToken string `json:"authorizationToken"`
}

// UploadLargeFile reads from r in LargeFilePartSize chunks and uploads
// each as a part. totalSize is used only for progress/log messages.
func (c *Client) UploadLargeFile(bucketID, fileName string, r io.Reader, totalSize int64, onProgress func(uploadedBytes, totalBytes int64)) error {
	var start startLargeFileResponse
	startReq := map[string]interface{}{
		"bucketId":    bucketID,
		"fileName":    fileName,
		"contentType": "b2/x-auto",
	}
	if err := c.apiPOST("b2_start_large_file", startReq, &start); err != nil {
		return fmt.Errorf("falha ao iniciar upload grande: %w", err)
	}

	var partSHA1s []string
	var uploaded int64
	buf := make([]byte, LargeFilePartSize)
	partNumber := 1

	for {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			partData := buf[:n]
			sum := sha1.Sum(partData)
			sha1Hex := hex.EncodeToString(sum[:])

			if err := c.uploadPartWithRetry(start.FileID, partNumber, partData, sha1Hex, 3); err != nil {
				_ = c.cancelLargeFile(start.FileID)
				return fmt.Errorf("falha ao enviar parte %d: %w", partNumber, err)
			}
			partSHA1s = append(partSHA1s, sha1Hex)
			uploaded += int64(n)
			if onProgress != nil {
				onProgress(uploaded, totalSize)
			}
			partNumber++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			_ = c.cancelLargeFile(start.FileID)
			return fmt.Errorf("falha ao ler dados para upload: %w", readErr)
		}
	}

	if len(partSHA1s) == 0 {
		_ = c.cancelLargeFile(start.FileID)
		return fmt.Errorf("arquivo vazio, nada para enviar")
	}

	finishReq := map[string]interface{}{
		"fileId":        start.FileID,
		"partSha1Array": partSHA1s,
	}
	if err := c.apiPOST("b2_finish_large_file", finishReq, nil); err != nil {
		_ = c.cancelLargeFile(start.FileID)
		return fmt.Errorf("falha ao finalizar upload grande: %w", err)
	}
	return nil
}

func (c *Client) uploadPartWithRetry(fileID string, partNumber int, data []byte, sha1Hex string, attempts int) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		var partURL getPartUploadURLResponse
		req := map[string]interface{}{"fileId": fileID}
		if err := c.apiPOST("b2_get_upload_part_url", req, &partURL); err != nil {
			lastErr = err
			continue
		}

		httpReq, err := http.NewRequest("POST", partURL.UploadURL, bytes.NewReader(data))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Authorization", partURL.AuthToken)
		httpReq.Header.Set("X-Bz-Part-Number", strconv.Itoa(partNumber))
		httpReq.Header.Set("X-Bz-Content-Sha1", sha1Hex)
		httpReq.ContentLength = int64(len(data))

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = parseAPIError(resp.StatusCode, body)
			continue
		}
		return nil
	}
	return lastErr
}

func (c *Client) cancelLargeFile(fileID string) error {
	req := map[string]interface{}{"fileId": fileID}
	return c.apiPOST("b2_cancel_large_file", req, nil)
}
