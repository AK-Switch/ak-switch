package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// AdminClient 封装与 akswitch server 通信的通用逻辑。
// 构造函数读取一次 host/port/token 并缓存，后续请求复用。
type AdminClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewAdminClient 创建客户端，自动检测 server 地址并加载 admin token。
// provider 参数用于 provider-scoped token；空字符串表示使用 any-admin-token。
func NewAdminClient(timeout time.Duration, provider string) (*AdminClient, error) {
	port := detectServerPort()
	host := detectServerHost()
	baseURL := fmt.Sprintf("http://%s:%d", host, port)

	token := ""
	var err error
	token, err = loadAdminToken(provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load admin token: %v\n", err)
	}

	return &AdminClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    baseURL,
		token:      token,
	}, nil
}

// Do 发送已构建的请求，自动注入 auth header。
func (c *AdminClient) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("Do: nil request")
	}
	if c.token != "" {
		req.Header.Set("X-Admin-Token", c.token)
	}
	return c.httpClient.Do(req)
}

// Get 发送 GET 请求。
func (c *AdminClient) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	return c.Do(req)
}

// Post 发送 POST 请求。
func (c *AdminClient) Post(path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	return c.Do(req)
}
