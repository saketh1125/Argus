package models

import (
	"strconv"
	"strings"
)

func itoa(i int) string { return strconv.Itoa(i) }

func normalizeLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
