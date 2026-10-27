package version

import (
	"strconv"
	"strings"
)

func Compare(a, b string) int {
	coreA, preA := split(a)
	coreB, preB := split(b)

	if diff := compareCore(coreA, coreB); diff != 0 {
		return diff
	}
	return comparePrerelease(preA, preB)
}

func split(value string) (core, prerelease string) {
	value = strings.TrimSpace(value)
	if len(value) > 1 && (value[0] == 'v' || value[0] == 'V') && value[1] >= '0' && value[1] <= '9' {
		value = value[1:]
	}
	if before, _, found := strings.Cut(value, "+"); found {
		value = before
	}
	if before, after, found := strings.Cut(value, "-"); found {
		return before, after
	}
	return value, ""
}

func compareCore(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	count := len(partsA)
	if len(partsB) > count {
		count = len(partsB)
	}
	for i := 0; i < count; i++ {
		if diff := comparePart(partAt(partsA, i), partAt(partsB, i)); diff != 0 {
			return diff
		}
	}
	return 0
}

func partAt(parts []string, index int) string {
	if index >= len(parts) {
		return "0"
	}
	return parts[index]
}

func comparePart(a, b string) int {
	numberA, errA := strconv.ParseUint(a, 10, 64)
	numberB, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return compareUint(numberA, numberB)
	case errA == nil:
		return 1
	case errB == nil:
		return -1
	default:
		return strings.Compare(a, b)
	}
}

func comparePrerelease(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	count := len(partsA)
	if len(partsB) > count {
		count = len(partsB)
	}
	for i := 0; i < count; i++ {
		if i >= len(partsA) {
			return -1
		}
		if i >= len(partsB) {
			return 1
		}
		numberA, errA := strconv.ParseUint(partsA[i], 10, 64)
		numberB, errB := strconv.ParseUint(partsB[i], 10, 64)
		switch {
		case errA == nil && errB == nil && numberA != numberB:
			return compareUint(numberA, numberB)
		case errA == nil && errB != nil:
			return -1
		case errA != nil && errB == nil:
			return 1
		case partsA[i] != partsB[i]:
			return strings.Compare(partsA[i], partsB[i])
		}
	}
	return 0
}

func compareUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
