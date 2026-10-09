// Package config отвечает за загрузку конфигурации сервиса из переменных
// окружения и флагов командной строки.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

// Config содержит настройки сервиса накопительной системы лояльности.
type Config struct {
	// RunAddress — адрес и порт, на котором запускается HTTP-сервер
	// (env RUN_ADDRESS, флаг -a).
	RunAddress string
	// DatabaseURI — адрес подключения к PostgreSQL
	// (env DATABASE_URI, флаг -d).
	DatabaseURI string
	// AccrualSystemAddress — адрес системы расчёта начислений
	// (env ACCRUAL_SYSTEM_ADDRESS, флаг -r).
	AccrualSystemAddress string
	TokenSecret          string
}

// Load читает конфигурацию из переменных окружения и флагов командной строки.
// Приоритет: флаг > переменная окружения > значение по умолчанию.
func Load() (*Config, error) {
	// Шаг 1: читаем env — эти значения станут дефолтами для флагов.
	databaseURI, databaseSet := os.LookupEnv(envDatabaseURI)
	accrualAddress, accrualSet := os.LookupEnv(envAccrualAddress)
	cfg := &Config{
		RunAddress:           getEnv(envRunAddress, defaultRunAddress),
		DatabaseURI:          databaseURI,
		AccrualSystemAddress: accrualAddress,
		TokenSecret:          getEnv(envTokenSecret, randomTokenSecret()),
	}

	// Шаг 2: регистрируем флаги. Их значения по умолчанию — то, что уже
	// прочитано из env. Явно переданный флаг перезапишет поле структуры.
	flag.StringVar(&cfg.RunAddress, "a", cfg.RunAddress, "адрес и порт запуска сервиса")
	flag.StringVar(&cfg.DatabaseURI, "d", cfg.DatabaseURI, "адрес подключения к базе данных")
	flag.StringVar(&cfg.AccrualSystemAddress, "r", cfg.AccrualSystemAddress, "адрес системы расчёта начислений")
	flag.Parse()

	// Шаг 3: валидация. Без этих параметров сервис работать не сможет,
	// поэтому падаем сразу при старте, а не в разгар работы (fail fast).
	// Важно: проверяем ПОСЛЕ flag.Parse(), чтобы учитывались и флаги.
	if cfg.DatabaseURI == "" {
		if databaseSet {
			return nil, fmt.Errorf("%s задана пустой строкой; укажите адрес или флаг -d", envDatabaseURI)
		}
		return nil, fmt.Errorf("не задан адрес подключения к базе данных (env %s или флаг -d)", envDatabaseURI)
	}
	if cfg.AccrualSystemAddress == "" {
		if accrualSet {
			return nil, fmt.Errorf("%s задана пустой строкой; укажите адрес или флаг -r", envAccrualAddress)
		}
		return nil, fmt.Errorf("не задан адрес системы расчёта начислений (env %s или флаг -r)", envAccrualAddress)
	}

	return cfg, nil
}

// Имена переменных окружения заданы требованиями дипломного проекта.
const (
	envRunAddress     = "RUN_ADDRESS"
	envDatabaseURI    = "DATABASE_URI"
	envAccrualAddress = "ACCRUAL_SYSTEM_ADDRESS"
	envTokenSecret    = "TOKEN_SECRET"

	defaultRunAddress = ":8080"
)

func randomTokenSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("ошибка генерации секретного ключа: %w", err))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// getEnv возвращает значение переменной окружения или fallback,
// если переменная не установлена или пуста.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
