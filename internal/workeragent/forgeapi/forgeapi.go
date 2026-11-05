package forgeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(httpClient *http.Client, baseURL string) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("http client is nil")
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("base URL is empty")
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Register(ctx context.Context, bootstrapToken string, input RegisterRequest) (RegisterResponse, error) {
	req, err := c.newRequest(ctx, bootstrapToken, "/internal/v1/workers/register", input)
	if err != nil {
		return RegisterResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RegisterResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RegisterResponse{}, parseError(resp)
	}

	var output RegisterResponse
	err = decodeResponse(resp, &output)
	return output, err
}

func (c *Client) Heartbeat(ctx context.Context, workerToken string, input HeartbeatRequest) (HeartbeatResponse, error) {
	req, err := c.newRequest(ctx, workerToken, "/internal/v1/workers/heartbeat", input)
	if err != nil {
		return HeartbeatResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HeartbeatResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return HeartbeatResponse{}, parseError(resp)
	}

	var output HeartbeatResponse
	err = decodeResponse(resp, &output)
	return output, err
}

func (c *Client) ClaimTask(ctx context.Context, workerToken string, input ClaimTaskRequest) (*ClaimTaskResponse, error) {
	req, err := c.newRequest(ctx, workerToken, "/internal/v1/tasks/claim", input)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var output ClaimTaskResponse
	err = decodeResponse(resp, &output)
	return &output, err
}

func (c *Client) RenewTaskLease(ctx context.Context, workerToken string, taskID uuid.UUID, input RenewTaskRequest) (RenewTaskResponse, error) {
	req, err := c.newRequest(ctx, workerToken, "/internal/v1/tasks/"+taskID.String()+"/renew", input)
	if err != nil {
		return RenewTaskResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RenewTaskResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RenewTaskResponse{}, parseError(resp)
	}

	var output RenewTaskResponse
	err = decodeResponse(resp, &output)
	return output, err
}

func (c *Client) CompleteTask(ctx context.Context, workerToken string, taskID uuid.UUID, input CompleteTaskRequest) error {
	req, err := c.newRequest(ctx, workerToken, "/internal/v1/tasks/"+taskID.String()+"/complete", input)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}

	return nil
}

func (c *Client) FailTask(ctx context.Context, workerToken string, taskID uuid.UUID, input FailTaskRequest) error {
	req, err := c.newRequest(ctx, workerToken, "/internal/v1/tasks/"+taskID.String()+"/fail", input)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}

	return nil
}

func (c *Client) newRequest(ctx context.Context, token string, path string, input any) (*http.Request, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))

	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func parseError(resp *http.Response) error {
	remoteErr := &RemoteError{
		StatusCode: resp.StatusCode,
	}

	var body errorResponse

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		remoteErr.Message = fmt.Sprintf("failed to decode error response: %v", err)
		return remoteErr
	}

	remoteErr.Code = body.Error.Code
	remoteErr.Message = body.Error.Message

	return remoteErr
}

func decodeResponse(resp *http.Response, output any) error {
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
