package chatops

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ChoiceKind categorises the result of ConfirmOrPick.
type ChoiceKind int

const (
	// ChoiceAbort: user said no / empty line / unparseable / read error.
	ChoiceAbort ChoiceKind = iota
	// ChoiceAll: user said yes — act on every candidate.
	ChoiceAll
	// ChoiceIndex: user entered a valid 1-based row number.
	ChoiceIndex
)

// Choice is the result of ConfirmOrPick. Index is populated only when
// Kind == ChoiceIndex.
type Choice struct {
	Kind  ChoiceKind
	Index int
}

// ConfirmOrPick prompts the user with y / N / row-number semantics.
//
//	"y" / "yes"                → ChoiceAll
//	an integer in [1, maxIndex] → ChoiceIndex
//	anything else / empty / EOF → ChoiceAbort
//
// Inputs are trimmed and lowercased. Out-of-range integers are treated as
// Abort — the caller decides whether to retry; in practice the user
// re-runs the command.
func ConfirmOrPick(prompt string, maxIndex int) Choice {
	fmt.Printf("%s [y/N/#]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return Choice{Kind: ChoiceAbort}
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return Choice{Kind: ChoiceAll}
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= maxIndex {
		return Choice{Kind: ChoiceIndex, Index: n}
	}
	return Choice{Kind: ChoiceAbort}
}
