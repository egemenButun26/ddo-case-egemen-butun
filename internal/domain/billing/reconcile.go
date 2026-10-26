package billing

import "parkakil/internal/domain/tariff"

type Txn struct {
	ProviderRef string       `json:"provider_ref"`
	Amount      tariff.Kurus `json:"amount_kurus"`
}

type AmountMismatch struct {
	ProviderRef string       `json:"provider_ref"`
	Ours        tariff.Kurus `json:"ours_kurus"`
	Provider    tariff.Kurus `json:"provider_kurus"`
}

type Report struct {
	Matched           []Txn            `json:"matched"`
	MissingInProvider []Txn            `json:"missing_in_provider"`
	MissingInOurs     []Txn            `json:"missing_in_ours"`
	AmountMismatch    []AmountMismatch `json:"amount_mismatch"`
	OurTotal          tariff.Kurus     `json:"our_total_kurus"`
	ProviderTotal     tariff.Kurus     `json:"provider_total_kurus"`
	Diff              tariff.Kurus     `json:"diff_kurus"`
}

func Reconcile(ourTxns []Txn, providerTxns []Txn) Report {
	report := Report{}
	oursByRef := make(map[string]Txn, len(ourTxns))
	providerByRef := make(map[string]Txn, len(providerTxns))

	for _, t := range ourTxns {
		report.OurTotal += t.Amount
		oursByRef[t.ProviderRef] = t
	}
	for _, t := range providerTxns {
		report.ProviderTotal += t.Amount
		providerByRef[t.ProviderRef] = t
	}
	report.Diff = report.ProviderTotal - report.OurTotal

	for _, our := range ourTxns {
		provider, ok := providerByRef[our.ProviderRef]
		if !ok {
			report.MissingInProvider = append(report.MissingInProvider, our)
			continue
		}
		if provider.Amount != our.Amount {
			report.AmountMismatch = append(report.AmountMismatch, AmountMismatch{
				ProviderRef: our.ProviderRef,
				Ours:        our.Amount,
				Provider:    provider.Amount,
			})
			continue
		}
		report.Matched = append(report.Matched, our)
	}

	for _, provider := range providerTxns {
		if _, ok := oursByRef[provider.ProviderRef]; !ok {
			report.MissingInOurs = append(report.MissingInOurs, provider)
		}
	}
	return report
}
