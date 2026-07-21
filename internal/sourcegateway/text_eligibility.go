package sourcegateway

import "unicode/utf8"

type textEligibilityScan struct {
	consumed   int
	ineligible bool
}

func scanTextEligibility(data []byte, terminal bool) textEligibilityScan {
	processed := 0
	for processed < len(data) {
		if data[processed] == 0 {
			return textEligibilityScan{consumed: processed, ineligible: true}
		}
		remaining := data[processed:]
		if !utf8.FullRune(remaining) {
			return textEligibilityScan{consumed: processed, ineligible: terminal}
		}
		value, size := utf8.DecodeRune(remaining)
		if value == utf8.RuneError && size == 1 {
			return textEligibilityScan{consumed: processed, ineligible: true}
		}
		processed += size
	}
	return textEligibilityScan{consumed: processed}
}
