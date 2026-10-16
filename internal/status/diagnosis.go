package status

import (
	"fmt"

	"github.com/leedcee/uplinkwatch/internal/checker"
)

// Diagnosis is a best-effort local explanation based on the latest target
// results. It deliberately avoids claiming that an ISP or remote service is
// at fault because a single probe cannot prove that on its own.
type Diagnosis struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Details string `json:"details"`
	Level   string `json:"level"`
}

func diagnose(snapshot Snapshot) Diagnosis {
	total := len(snapshot.Results)
	if total == 0 {
		return Diagnosis{
			Code:    "waiting",
			Title:   "Ожидание данных",
			Details: "Первая локальная проверка ещё не завершилась.",
			Level:   "waiting",
		}
	}

	down := make([]checker.Result, 0, total)
	for _, result := range snapshot.Results {
		if !result.Reachable {
			down = append(down, result)
		}
	}
	if len(down) == 0 {
		return Diagnosis{
			Code:    "healthy",
			Title:   "Локальное подключение работает",
			Details: fmt.Sprintf("Все проверенные сайты доступны: %d из %d.", total, total),
			Level:   "healthy",
		}
	}
	if total == 1 {
		return Diagnosis{
			Code:    "single_target_failure",
			Title:   "Сайт не прошёл проверку",
			Details: "Одного сайта недостаточно, чтобы отличить его сбой от проблемы локального подключения.",
			Level:   "warning",
		}
	}
	if len(down) < total {
		return Diagnosis{
			Code:    "partial_failure",
			Title:   "Проблема затрагивает отдельные сайты",
			Details: fmt.Sprintf("Доступны %d из %d сайтов. Локальный интернет в целом работает.", total-len(down), total),
			Level:   "warning",
		}
	}

	stage := commonFailureStage(down)
	switch stage {
	case checker.StageDNS:
		return Diagnosis{
			Code:    "dns_failure",
			Title:   "Вероятна проблема с DNS",
			Details: "Имена всех сайтов не разрешаются в IP-адреса. Проверьте DNS и подключение к интернету.",
			Level:   "critical",
		}
	case checker.StageTCP:
		return Diagnosis{
			Code:    "connection_failure",
			Title:   "Вероятна проблема с подключением",
			Details: "DNS работает, но соединение не установлено ни с одним сайтом. Проверьте интернет, роутер и файрвол.",
			Level:   "critical",
		}
	case checker.StageTLS:
		return Diagnosis{
			Code:    "tls_failure",
			Title:   "Вероятна проблема с защищённым соединением",
			Details: "Все проверки остановились на TLS. Возможны неверные системные часы, сертификаты или перехват HTTPS.",
			Level:   "critical",
		}
	case checker.StageHTTP, checker.StageContent:
		return Diagnosis{
			Code:    "http_failure",
			Title:   "Сайты отвечают, но проверки отклонены",
			Details: "Сеть, DNS и соединение работают. Причину нужно искать в HTTP-ответах или ожидаемом содержимом.",
			Level:   "warning",
		}
	default:
		return Diagnosis{
			Code:    "multiple_failures",
			Title:   "Обнаружено несколько типов ошибок",
			Details: "Все сайты недоступны, но причины различаются. Посмотрите этап и причину напротив каждого сайта.",
			Level:   "critical",
		}
	}
}

func commonFailureStage(results []checker.Result) checker.Stage {
	var common checker.Stage
	for _, result := range results {
		if result.Failure == nil {
			return ""
		}
		if common == "" {
			common = result.Failure.Stage
			continue
		}
		if result.Failure.Stage != common {
			return ""
		}
	}
	return common
}
