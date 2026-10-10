package model

import (
	"fmt"
	"strconv"
	"strings"
)

type Money int64

func ParseMoney(value string) (Money, error) {
	digits := value
	sign := ""

	if strings.HasPrefix(digits, "-") {
		sign = "-"
		digits = digits[1:]
	}
	whole, fraction, hasDots := strings.Cut(digits, ".")
	if whole == "" || (hasDots && fraction == "") {
		return 0, fmt.Errorf("invalid money: %q", value)
	}
	for _, c := range whole + fraction {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("неверный формат суммы: %q", value)
		}
	}
	fraction = strings.TrimRight(fraction, "0")
	if len(fraction) > 2 {
		return 0, fmt.Errorf("сумма должна быть кратна 0.01: %q", value)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}

	units, err := strconv.ParseInt(sign+whole+fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("преобразование суммы %q: %w", value, err)
	}

	return Money(units), nil

}

func (m Money) String() string {

	digits := strconv.FormatInt(int64(m), 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign = "-"
		digits = digits[1:]
	}
	for len(digits) < 3 {
		digits = "0" + digits
	}
	point := len(digits) - 2
	return sign + digits[:point] + "." + digits[point:]
}
