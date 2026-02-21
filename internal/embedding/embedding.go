package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultModel = "gemini-embedding-001"
	APIURL       = "https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s"
)

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

type embedRequest struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

type embedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  DefaultModel,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) GetEmbedding(text string) ([]float32, error) {
	url := fmt.Sprintf(APIURL, c.model, c.apiKey)
	
	reqBody := embedRequest{}
	reqBody.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned status %d", resp.StatusCode)
	}

	var res embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return res.Embedding.Values, nil
}
