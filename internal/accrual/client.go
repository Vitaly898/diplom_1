package accrual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
)

const (
	maxAttempts   = 3
	retryInterval = 100 * time.Millisecond
)

var ErrNotRegistered = errors.New("заказ ещё не зарегистрирован в accrual")

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("accrual ограничил запросы на %s", e.RetryAfter)
}

type Result struct {
	Status  string
	Accrual *model.Money
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(address string) (*Client, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("неверный адрес системы начислений")
	}
	return &Client{
		baseURL:    strings.TrimRight(address, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func (c *Client) GetOrder(ctx context.Context, number string) (Result, error) {
	address := c.baseURL + "/api/orders/" + url.PathEscape(number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return Result{}, fmt.Errorf("создание запроса начислений: %w", err)
	}
	var resp *http.Response
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = c.httpClient.Do(req)
		if ctx.Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return Result{}, ctx.Err()
		}
		retry := err != nil || (resp.StatusCode >= 500 && resp.StatusCode <= 599)
		if !retry || attempt == maxAttempts-1 {
			break
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
		}
		timer := time.NewTimer(time.Duration(attempt+1) * retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("запрос начислений: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		switch resp.StatusCode {
		case http.StatusNoContent:
			return Result{}, ErrNotRegistered
		case http.StatusTooManyRequests:
			return Result{}, &RateLimitError{
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			}
		default:
			return Result{}, fmt.Errorf("система начислений вернула HTTP %d", resp.StatusCode)
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return Result{}, fmt.Errorf("чтение ответа начислений: %w", err)
	}
	if len(body) > 64*1024 {
		return Result{}, errors.New("слишком большой ответ системы начислений")
	}
	var response struct {
		Order   string          `json:"order"`
		Status  string          `json:"status"`
		Accrual json.RawMessage `json:"accrual"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Result{}, fmt.Errorf("разбор ответа начислений: %w", err)
	}
	if response.Order != number {
		return Result{}, errors.New("номер заказа в ответе начислений не совпадает с запросом")
	}
	result := Result{}
	switch response.Status {
	case "REGISTERED", model.StatusProcessing:
		result.Status = model.StatusProcessing
	case model.StatusInvalid:
		result.Status = model.StatusInvalid
	case model.StatusProcessed:
		result.Status = model.StatusProcessed
		if len(response.Accrual) > 0 && string(response.Accrual) != "null" {
			amount, err := model.ParseMoney(string(response.Accrual))
			if err != nil {
				return Result{}, fmt.Errorf("разбор суммы начисления: %w", err)
			}
			if amount < 0 || amount > 999999999999 {
				return Result{}, errors.New("сумма начисления вне диапазона NUMERIC(12,2)")
			}
			result.Accrual = &amount
		}
	default:
		return Result{}, fmt.Errorf("неизвестный статус начислений: %q", response.Status)
	}
	return result, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err == nil && seconds >= 0 && seconds <= int64((1<<63-1)/time.Second) {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		if delay := date.Sub(now); delay > 0 {
			return delay
		}
		return 0
	}
	slog.Warn("не удалось разобрать Retry-After, используется значение по умолчанию", "retry_after", value, "fallback", time.Minute)
	return time.Minute
}
