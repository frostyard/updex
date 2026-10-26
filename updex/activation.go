package updex

import (
	"slices"

	"github.com/frostyard/updex/v2/config"
)

// featureTransferSplit is the transfers associated with one feature, split by
// the sysupdate.d(5) activation rule (ADR-0017).
type featureTransferSplit struct {
	// active holds the transfers that are active: the ones a command acts on
	// (downloads, links, vacuums, holds by rollback) when the feature is
	// forced enabled, or the ones that stay active without the feature when
	// it is forced disabled.
	active []*config.Transfer

	// inactive holds the other associated transfers, with the reason each
	// is not active.
	inactive []inactiveAssociation
}

// inactiveAssociation is an associated transfer that is not active.
type inactiveAssociation struct {
	transfer *config.Transfer
	report   InactiveTransfer
}

// splitFeatureTransfers splits the transfers associated with feature name
// (config.GetTransfersForFeature: the name is in Features= or in
// RequisiteFeatures=) into active and inactive, using the enabled features of
// the loaded domain with name forced to enabled. That is the set a command
// acts on: a feature command never downloads, links, removes or holds by
// rollback a transfer that is not active, whatever its association.
//
// forceEnabled is the state name is given, whatever its drop-ins say:
// enabled for update, check, enable, rollback and the status read (the
// command acts as if the feature were enabled; enable runs after the drop-in
// is written), disabled for disable (what stays active without the feature).
func splitFeatureTransfers(features []*config.Feature, transfers []*config.Transfer, name string, forceEnabled bool) featureTransferSplit {
	enabled := config.EnabledFeatureSet(features)
	if forceEnabled {
		enabled[name] = true
	} else {
		delete(enabled, name)
	}

	var split featureTransferSplit
	for _, t := range config.GetTransfersForFeature(transfers, name) {
		a := config.CheckTransferActivation(t, enabled)
		if a.Active {
			split.active = append(split.active, t)
			continue
		}
		split.inactive = append(split.inactive, inactiveAssociation{
			transfer: t,
			report: InactiveTransfer{
				Component:         t.Component,
				MissingRequisites: a.MissingRequisites,
				NeedsOneOf:        a.NeedsOneOf,
			},
		})
	}
	return split
}

// associated returns how many transfers are associated with the feature.
func (s featureTransferSplit) associated() int {
	return len(s.active) + len(s.inactive)
}

// inactiveReports returns the report form of the inactive transfers, nil when
// there are none.
func (s featureTransferSplit) inactiveReports() []InactiveTransfer {
	if len(s.inactive) == 0 {
		return nil
	}
	out := make([]InactiveTransfer, len(s.inactive))
	for i, in := range s.inactive {
		out[i] = in.report
	}
	return out
}

// inactiveTransfers returns the inactive transfers themselves.
func (s featureTransferSplit) inactiveTransfers() []*config.Transfer {
	out := make([]*config.Transfer, len(s.inactive))
	for i, in := range s.inactive {
		out[i] = in.transfer
	}
	return out
}

// keptTransfers reports the active transfers of a split computed with the
// feature forced disabled: the other enabled features of each transfer's
// Features= keep it active.
func (s featureTransferSplit) keptTransfers(features []*config.Feature, name string) []KeptTransfer {
	if len(s.active) == 0 {
		return nil
	}
	enabled := config.EnabledFeatureSet(features)
	out := make([]KeptTransfer, 0, len(s.active))
	for _, t := range s.active {
		var through []string
		for _, f := range t.Transfer.Features {
			if f != name && enabled[f] && !slices.Contains(through, f) {
				through = append(through, f)
			}
		}
		out = append(out, KeptTransfer{Component: t.Component, ActiveThrough: through})
	}
	return out
}
