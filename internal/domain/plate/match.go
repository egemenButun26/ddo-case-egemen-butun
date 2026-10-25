package plate

import (
	"errors"
	"sort"
)

// Thresholds karar eşikleridir (binde).
type Thresholds struct {
	Auto   int
	Review int
	Margin int
}

// DefaultThresholds varsayılan eşiklerdir.
var DefaultThresholds = Thresholds{Auto: 900, Review: 700, Margin: 100}

// Validate eşiklerin tutarlı olduğunu denetler.
func (t Thresholds) Validate() error {
	if t.Review < 0 || t.Auto < 0 || t.Margin < 0 {
		return errors.New("eşikler negatif olamaz")
	}
	if t.Review > 1000 || t.Auto > 1000 || t.Margin > 1000 {
		return errors.New("eşikler 1000'i aşamaz")
	}
	if t.Review > t.Auto {
		return errors.New("inceleme eşiği otomatik eşiğini aşamaz")
	}
	return nil
}

// Decision eşleştirme kararıdır.
type Decision string

const (
	DecisionExact  Decision = "exact"
	DecisionAuto   Decision = "auto"
	DecisionReview Decision = "review"
	DecisionNone   Decision = "none"
)

// Candidate eşleştirilecek aday plakadır. Plate ham olabilir.
type Candidate struct {
	ID    string
	Plate string
}

// Result eşleştirme sonucudur; inceleme kuyruğu ne önerildiğini görebilsin
// diye Best ve RunnerUp aday varsa her zaman doldurulur.
type Result struct {
	Decision           Decision
	Reason             string
	Read               string
	Best               *Candidate
	Confidence         int
	RunnerUp           *Candidate
	RunnerUpConfidence int
}

type puanli struct {
	aday Candidate
	conf int
}

// Match okumayı adaylarla eşleştirir. Saf fonksiyondur; yan etkisi yoktur.
func Match(read string, candidates []Candidate, t Thresholds) (Result, error) {
	if err := t.Validate(); err != nil {
		return Result{}, err
	}

	okuma := Normalize(read)
	if okuma == "" {
		return Result{Decision: DecisionNone, Reason: "empty_read", Read: ""}, nil
	}
	if len(candidates) == 0 {
		return Result{Decision: DecisionNone, Reason: "no_candidates", Read: okuma}, nil
	}

	liste := make([]puanli, 0, len(candidates))
	for _, a := range candidates {
		liste = append(liste, puanli{aday: a, conf: Confidence(okuma, Normalize(a.Plate))})
	}
	sort.Slice(liste, func(i, j int) bool {
		if liste[i].conf != liste[j].conf {
			return liste[i].conf > liste[j].conf
		}
		return liste[i].aday.ID < liste[j].aday.ID
	})

	enIyi := liste[0].aday
	enIyiConf := liste[0].conf
	var ikinci *Candidate
	ikinciConf := 0
	if len(liste) > 1 {
		kopya := liste[1].aday
		ikinci = &kopya
		ikinciConf = liste[1].conf
	}

	tamEslesen := 0
	for _, a := range candidates {
		if Normalize(a.Plate) == okuma {
			tamEslesen++
		}
	}
	if tamEslesen == 1 {
		return Result{
			Decision:           DecisionExact,
			Reason:             "exact",
			Read:               okuma,
			Best:               &enIyi,
			Confidence:         1000,
			RunnerUp:           ikinci,
			RunnerUpConfidence: ikinciConf,
		}, nil
	}
	if tamEslesen > 1 {
		return Result{
			Decision:           DecisionReview,
			Reason:             "ambiguous_exact",
			Read:               okuma,
			Best:               &enIyi,
			Confidence:         enIyiConf,
			RunnerUp:           ikinci,
			RunnerUpConfidence: ikinciConf,
		}, nil
	}

	if enIyiConf < t.Review {
		return Result{
			Decision:           DecisionNone,
			Reason:             "low_confidence",
			Read:               okuma,
			Best:               &enIyi,
			Confidence:         enIyiConf,
			RunnerUp:           ikinci,
			RunnerUpConfidence: ikinciConf,
		}, nil
	}

	if enIyiConf >= t.Auto && (ikinci == nil || enIyiConf-ikinciConf >= t.Margin) && ValidTR(okuma) {
		return Result{
			Decision:           DecisionAuto,
			Reason:             "auto",
			Read:               okuma,
			Best:               &enIyi,
			Confidence:         enIyiConf,
			RunnerUp:           ikinci,
			RunnerUpConfidence: ikinciConf,
		}, nil
	}

	neden := "low_confidence_review"
	if ikinci != nil && enIyiConf-ikinciConf < t.Margin {
		neden = "ambiguous"
	} else if !ValidTR(okuma) {
		neden = "invalid_format"
	}

	return Result{
		Decision:           DecisionReview,
		Reason:             neden,
		Read:               okuma,
		Best:               &enIyi,
		Confidence:         enIyiConf,
		RunnerUp:           ikinci,
		RunnerUpConfidence: ikinciConf,
	}, nil
}
