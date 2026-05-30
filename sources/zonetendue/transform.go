package zonetendue

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
)

// rawCSVName is the datadir filename for the upstream raw input.
const rawCSVName = "zonage_tlv.raw.csv"

// rawCSVURL is the "Zonage TLV" CSV published on data.gouv.fr (dataset slug
// liste-des-communes-selon-le-zonage-tlv-1). data.gouv mints a dated static
// path per revision; bump this when a new décret is published.
const rawCSVURL = "https://static.data.gouv.fr/resources/liste-des-communes-selon-le-zonage-tlv-1/20251230-094759/zonage-tlv-decret-22-dec-2025.csv"

const metaSource = "data.gouv.fr/datasets/liste-des-communes-selon-le-zonage-tlv-1"

// dateRE extracts a DD/MM/YYYY date from the current-zonage column header
// ("Zonage TLV post décret 22/12/2025").
var dateRE = regexp.MustCompile(`(\d{1,2})/(\d{1,2})/(\d{4})`)

// transform rebuilds the processed zonage_tlv artifact from the upstream
// CSV. Columns: CODGEO25;DEP;LIBGEO;Code EPCI;Libellé EPCI;Zonage TLV 2013;
// Zonage TLV 2023;Zonage TLV post décret <date>. Only the post-décret column
// (matched by its "Zonage TLV post" header prefix) drives the current tier;
// the "Zonage TLV 2013" column drives the historical flag. Non-tendue
// communes are dropped — absence from the compact file means non_tendue.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawCSVName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("zonetendue: read header: %w", err)
	}
	codgeo, col2013, colCur := -1, -1, -1
	for i, h := range header {
		switch h := strings.TrimSpace(h); {
		case strings.HasPrefix(strings.ToUpper(h), "CODGEO"):
			codgeo = i
		case strings.EqualFold(h, "Zonage TLV 2013"):
			col2013 = i
		case strings.HasPrefix(strings.ToLower(h), "zonage tlv post"):
			colCur = i
		}
	}
	if codgeo < 0 || col2013 < 0 || colCur < 0 {
		return fmt.Errorf("zonetendue: header missing CODGEO/TLV-2013/post-décret columns: %v", header)
	}

	idx := Index{
		Meta: Meta{
			Source:        metaSource,
			DownloadedAt:  time.Now().UTC().Format("2006-01-02"),
			EffectiveDate: parseSlashDate(header[colCur]),
			Note:          "Zonage TLV per " + strings.TrimSpace(header[colCur]) + ". Communes absent from the compact file are implicitly non_tendue.",
		},
		Communes: map[string]Entry{},
	}
	total := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("zonetendue: read row: %w", err)
		}
		insee := strings.TrimSpace(rec[codgeo])
		if insee == "" {
			continue
		}
		total++
		tier := classifyTLV(rec[colCur])
		if tier == TierNonTendue {
			continue // non-tendue communes are implicit; keep the file compact
		}
		idx.Communes[insee] = Entry{
			TLV2013: strings.EqualFold(strings.TrimSpace(rec[col2013]), "TLV"),
			Tier:    tier,
		}
	}
	idx.Meta.RowCountCommunes = total
	idx.Meta.RowCountKept = len(idx.Communes)
	if len(idx.Communes) == 0 {
		return errors.New("zonetendue: transform produced no tendue communes")
	}

	return json.NewEncoder(dst).Encode(idx)
}

// classifyTLV maps an upstream "Zonage TLV post décret …" cell to a Tier.
// The cells are numbered labels: "1. Zone tendue", "2. Zone touristique et
// tendue", "3. Non tendue". Keying off the leading number keeps it resilient
// to minor label wording changes.
func classifyTLV(cell string) Tier {
	switch c := strings.TrimSpace(cell); {
	case strings.HasPrefix(c, "1."):
		return TierTendue
	case strings.HasPrefix(c, "2."):
		return TierTendueTouristique
	default:
		return TierNonTendue
	}
}

// validate gates publication: the rebuilt artifact must parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.CountTendue() == 0 {
		return errors.New("zonetendue: validated artifact has no communes")
	}
	return nil
}

// parseSlashDate turns "...22/12/2025" into "2025-12-22"; "" when absent.
func parseSlashDate(s string) string {
	m := dateRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	return fmt.Sprintf("%s-%02d-%02d", m[3], month, day)
}
