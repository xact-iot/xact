package reporting

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

const ptToMM = 0.352778 // 1 PDF point = 0.352778 mm

// TemplateDoc is the top-level structure of a report template.
type TemplateDoc struct {
	Config   DocConfig `json:"config"`
	Elements []Element `json:"elements"`
}

// DocConfig holds page-level settings. Margins are in PDF points.
type DocConfig struct {
	PageSize      string  `json:"pageSize"`    // "A4", "Letter", "Legal"
	Orientation   string  `json:"orientation"` // "P" or "L"
	Margins       Margins `json:"margins"`
	DocumentTitle string  `json:"documentTitle"`
	Watermark     string  `json:"watermark,omitempty"`
}

// Margins holds page margin values in PDF points (72pt = 1 inch).
type Margins struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// Element is one content block in the report.
type Element struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"` // "title" | "table" | "spacer" | "footer" | "image"

	// Title and Table: grid of cells.
	Rows       [][]CellProps `json:"rows,omitempty"`
	ColWidths  []float64     `json:"colWidths,omitempty"`  // relative weight per column
	RowHeights []float64     `json:"rowHeights,omitempty"` // height multiplier per row
	BgColor    string        `json:"bgColor,omitempty"`    // default cell background
	TextColor  string        `json:"textColor,omitempty"`  // default cell text colour
	AllBorders bool          `json:"allBorders,omitempty"`
	NoBorders  bool          `json:"noBorders,omitempty"`

	// Spacer and Image height.
	Height float64 `json:"height,omitempty"` // pts

	// Footer.
	Text          string   `json:"text,omitempty"`
	Font          string   `json:"font,omitempty"`
	Size          float64  `json:"size,omitempty"`
	Bold          bool     `json:"bold,omitempty"`
	Italic        bool     `json:"italic,omitempty"`
	Underline     bool     `json:"underline,omitempty"`
	Align         string   `json:"align,omitempty"` // "L", "C", "R"
	TextColor2    string   `json:"textColor2,omitempty"`
	FooterBorders *Borders `json:"footerBorders,omitempty"`

	// Image.
	ImageData string  `json:"imageData,omitempty"` // base64 data URL
	Width     float64 `json:"width,omitempty"`     // pts (0 = full content width)

	// Chart.
	ChartConfig *ChartConfig `json:"chartConfig,omitempty"`

	// Events.
	EventsConfig *EventsConfig `json:"eventsConfig,omitempty"`
}

// EventsConfig holds filter settings for an events-list report element.
type EventsConfig struct {
	Severity string   `json:"severity,omitempty"` // "" = all
	Device   string   `json:"device,omitempty"`
	Search   string   `json:"search,omitempty"`
	Lookback string   `json:"lookback"` // "1h","3h","6h","12h","24h","7d","30d"
	Limit    int      `json:"limit"`
	Columns  []string `json:"columns"` // subset of: timestamp,severity,user,device,message,params
}

// CellProps defines the appearance of a single table or title cell.
type CellProps struct {
	Text       string   `json:"text"`
	Font       string   `json:"font,omitempty"`
	Size       float64  `json:"size,omitempty"`
	Bold       bool     `json:"bold,omitempty"`
	Italic     bool     `json:"italic,omitempty"`
	Underline  bool     `json:"underline,omitempty"`
	Align      string   `json:"align,omitempty"` // "L", "C", "R"
	BgColor    string   `json:"bgColor,omitempty"`
	TextColor  string   `json:"textColor,omitempty"`
	Borders    *Borders `json:"borders,omitempty"`
	CellWidth  float64  `json:"cellWidth,omitempty"`  // pts override
	CellHeight float64  `json:"cellHeight,omitempty"` // pts override
	Wrap       bool     `json:"wrap,omitempty"`
	LinkURL    string   `json:"linkUrl,omitempty"`
	ImageData  string   `json:"imageData,omitempty"`    // base64 data URL
	ImageFit   string   `json:"imageFit,omitempty"`     // "contain" or "stretch"
	ImagePad   float64  `json:"imagePadding,omitempty"` // pts
}

// Borders defines per-side border widths in pts (0 = no border).
type Borders struct {
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
}

// GenerateContext carries runtime dependencies needed during PDF generation.
type GenerateContext struct {
	OrgName string
	// TagPathsQueryer fetches time-series data for a list of full tag paths below
	// the org node (e.g. "NASA.ISS.life.clean_water"). Intermediate RTDB nodes are
	// handled by the implementation. May be nil - chart elements render as blank space.
	TagPathsQueryer func(ctx context.Context, orgName string, tagPaths []string, start, end time.Time) ([]sqldb.MetricSeries, error)
	// EventsQueryer fetches event log entries. May be nil - events elements render as blank space.
	EventsQueryer func(ctx context.Context, filter sqldb.EventFilter) ([]events.EventEntry, error)
}

// GeneratePDF renders the template JSON to PDF bytes.
// All {{variable}} tokens must already be substituted before calling this.
func GeneratePDF(ctx context.Context, templateJSON json.RawMessage, gc GenerateContext) ([]byte, error) {
	var doc TemplateDoc
	if err := json.Unmarshal(templateJSON, &doc); err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	return renderPDF(ctx, &doc, gc)
}

func renderPDF(ctx context.Context, doc *TemplateDoc, gc GenerateContext) ([]byte, error) {
	cfg := doc.Config
	pageSize := cfg.PageSize
	if pageSize == "" {
		pageSize = "A4"
	}
	orient := cfg.Orientation
	if orient == "" {
		orient = "P"
	}

	pdf := fpdf.New(orient, "mm", pageSize, "")
	pdf.AliasNbPages("«PAGE_COUNT»")
	pdf.SetTitle(cfg.DocumentTitle, false)

	m := cfg.Margins
	lm := ptOrDefault(m.Left, 72) * ptToMM
	rm := ptOrDefault(m.Right, 72) * ptToMM
	tm := ptOrDefault(m.Top, 72) * ptToMM
	pdf.SetMargins(lm, tm, rm)

	pageW, pageH := pdf.GetPageSize()
	contentW := pageW - lm - rm

	// Collect footer elements - rendered via SetFooterFunc, not inline.
	var footerEls []*Element
	for i := range doc.Elements {
		if doc.Elements[i].Type == "footer" {
			footerEls = append(footerEls, &doc.Elements[i])
		}
	}
	footerH := float64(len(footerEls)) * 7.0 // 7 mm per footer cell

	bm := ptOrDefault(m.Bottom, 72) * ptToMM
	if footerH > 0 && footerH+4 > bm {
		bm = footerH + 4
	}

	if len(footerEls) > 0 {
		pdf.SetFooterFunc(func() {
			pdf.SetY(-footerH)
			for _, el := range footerEls {
				renderFooter(pdf, el, contentW)
			}
		})
	}

	pdf.AddPage()
	pdf.SetAutoPageBreak(true, bm)

	for i := range doc.Elements {
		el := &doc.Elements[i]
		switch el.Type {
		case "title":
			if i+1 < len(doc.Elements) && doc.Elements[i+1].Type == "table" {
				keepH := gridRowsHeight(el, len(el.Rows)) + gridRowsHeight(&doc.Elements[i+1], 2)
				ensureVerticalSpace(pdf, keepH, pageH, bm)
			}
			renderGrid(pdf, el, contentW, 16)
		case "table":
			ensureVerticalSpace(pdf, gridRowsHeight(el, 2), pageH, bm)
			renderGrid(pdf, el, contentW, 10)
		case "spacer":
			h := el.Height * ptToMM
			if h <= 0 {
				h = 10
			}
			ensureVerticalSpace(pdf, h, pageH, bm)
			pdf.Ln(h)
		case "footer":
			// Rendered via SetFooterFunc - skip inline.
		case "image":
			h := ptOrDefault(el.Height, 144) * ptToMM
			ensureVerticalSpace(pdf, h, pageH, bm)
			if el.ImageData != "" {
				embedBase64Image(pdf, el, lm, contentW)
			} else if el.Height > 0 {
				pdf.Ln(h)
			}
		case "events":
			if el.EventsConfig != nil && gc.EventsQueryer != nil {
				renderEventsTable(ctx, pdf, el, contentW, gc)
			}
		case "chart":
			heightPt := ptOrDefault(el.Height, 200)
			heightMM := heightPt * ptToMM
			ensureVerticalSpace(pdf, heightMM, pageH, bm)
			if el.ChartConfig != nil && len(el.ChartConfig.Metrics) > 0 && gc.TagPathsQueryer != nil {
				end := time.Now()
				start := end.Add(-parseLookback(el.ChartConfig.Lookback))

				// Collect non-empty tag paths.
				var paths []string
				for _, p := range el.ChartConfig.Metrics {
					p = strings.TrimSpace(p)
					if p != "" {
						paths = append(paths, p)
					}
				}

				log.Printf("[chart] querying org=%q paths=%v window=%v–%v",
					gc.OrgName, paths, start.Format(time.RFC3339), end.Format(time.RFC3339))
				allSeries, err := gc.TagPathsQueryer(ctx, gc.OrgName, paths, start, end)
				if err != nil {
					log.Printf("[chart] query error: %v", err)
				} else {
					log.Printf("[chart] returned %d series", len(allSeries))
				}

				if len(allSeries) > 0 {
					widthPt := el.Width
					if widthPt <= 0 {
						widthPt = contentW / ptToMM
					}
					const dpi = 150.0
					widthPx := int(widthPt * dpi / 72)
					heightPx := int(heightPt * dpi / 72)
					if pngBytes, err := RenderChart(*el.ChartConfig, allSeries, widthPx, heightPx); err == nil {
						imgName := "chart_" + el.ID
						pdf.RegisterImageOptionsReader(imgName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(pngBytes))
						wMM := contentW
						if el.Width > 0 {
							wMM = el.Width * ptToMM
						}
						y := pdf.GetY()
						pdf.ImageOptions(imgName, lm, y, wMM, heightMM, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
						pdf.SetY(y + heightMM)
						break
					} else {
						log.Printf("[chart] RenderChart failed: %v", err)
					}
				} else {
					log.Printf("[chart] no series data returned - chart element %q will render as blank space", el.ID)
				}
			}
			pdf.Ln(heightMM)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("rendering pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// embedBase64Image decodes a data URL image and places it in the PDF at the current Y position.
func embedBase64Image(pdf *fpdf.Fpdf, el *Element, lm, contentW float64) {
	imgBytes, imgType, ok := decodeImageDataURL(el.ImageData)
	if !ok {
		return
	}
	opts := fpdf.ImageOptions{ImageType: imgType}
	imgName := "img_" + el.ID
	pdf.RegisterImageOptionsReader(imgName, opts, bytes.NewReader(imgBytes))
	wMM := contentW
	if el.Width > 0 {
		wMM = el.Width * ptToMM
	}
	hMM := ptOrDefault(el.Height, 144) * ptToMM
	y := pdf.GetY()
	pdf.ImageOptions(imgName, lm, y, wMM, hMM, false, opts, 0, "")
	pdf.SetY(y + hMM)
}

func decodeImageDataURL(dataURL string) ([]byte, string, bool) {
	commaIdx := strings.Index(dataURL, ",")
	if commaIdx < 0 {
		return nil, "", false
	}
	imgBytes, err := base64.StdEncoding.DecodeString(dataURL[commaIdx+1:])
	if err != nil {
		return nil, "", false
	}
	imgType := "PNG"
	header := strings.ToLower(dataURL[:commaIdx])
	if strings.Contains(header, "jpeg") || strings.Contains(header, "jpg") {
		imgType = "JPG"
	} else if strings.Contains(header, "gif") {
		imgType = "GIF"
	}
	return imgBytes, imgType, true
}

// renderGrid renders a title or table element as a grid of cells.
func renderGrid(pdf *fpdf.Fpdf, el *Element, contentW, defaultFontSize float64) {
	if len(el.Rows) == 0 {
		return
	}
	numCols := 0
	for _, row := range el.Rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if numCols == 0 {
		return
	}

	// Build column widths from relative weights.
	weights := make([]float64, numCols)
	for i := range weights {
		if i < len(el.ColWidths) && el.ColWidths[i] > 0 {
			weights[i] = el.ColWidths[i]
		} else {
			weights[i] = 1
		}
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	colW := make([]float64, numCols)
	for i, w := range weights {
		colW[i] = contentW * w / total
	}

	_, pageH := pdf.GetPageSize()
	_, _, _, bm := pdf.GetMargins()
	for ri, row := range el.Rows {
		rowH := gridRowHeight(el, ri)
		ensureVerticalSpace(pdf, rowH, pageH, bm)

		rowStartX := pdf.GetX()
		for ci := 0; ci < numCols; ci++ {
			var cell CellProps
			if ci < len(row) {
				cell = row[ci]
			}

			// Cell dimensions.
			w := colW[ci]
			if cell.CellWidth > 0 {
				w = cell.CellWidth * ptToMM
			}
			h := rowH
			if cell.CellHeight > 0 {
				h = cell.CellHeight * ptToMM
			}

			// Background colour.
			setBg(pdf, coalesce(cell.BgColor, el.BgColor, "#ffffff"))

			// Text colour.
			setTx(pdf, coalesce(cell.TextColor, el.TextColor, "#000000"))

			// Font.
			fam := coalesce(cell.Font, "Helvetica")
			sz := cell.Size
			if sz == 0 {
				sz = defaultFontSize
			}
			style := fontStyle(cell.Bold, cell.Italic, cell.Underline)
			pdf.SetFont(fam, style, sz)

			// Alignment.
			align := strings.ToUpper(coalesce(cell.Align, "L"))

			// Border string.
			border := gridBorder(el.AllBorders, el.NoBorders, cell.Borders)

			ln := 0
			if ci == numCols-1 {
				ln = 1
			}

			x, y := pdf.GetX(), pdf.GetY()
			text := resolvePageVars(pdf, cell.Text)
			imgName, imgType, imgInfo := registerCellImage(pdf, el, ri, ci, &cell)
			if imgInfo != nil {
				drawImageCellFrame(pdf, x, y, w, h, border)
				drawCellImage(pdf, imgName, imgType, imgInfo, &cell, x, y, w, h)
				if ln > 0 {
					if ln == 1 {
						pdf.SetXY(rowStartX, y+h)
					} else {
						pdf.SetXY(x+w, y+h)
					}
				} else {
					pdf.SetXY(x+w, y)
				}
			} else {
				pdf.CellFormat(w, h, text, border, ln, align, true, 0, "")
			}
		}
	}
}

func gridRowsHeight(el *Element, maxRows int) float64 {
	if el == nil || maxRows <= 0 {
		return 0
	}
	rows := len(el.Rows)
	if rows > maxRows {
		rows = maxRows
	}
	total := 0.0
	for ri := 0; ri < rows; ri++ {
		total += gridRowHeight(el, ri)
	}
	return total
}

func gridRowHeight(el *Element, rowIndex int) float64 {
	const defaultRowH = 7.0 // mm

	rowH := defaultRowH
	if rowIndex < len(el.RowHeights) && el.RowHeights[rowIndex] > 0 {
		rowH = defaultRowH * el.RowHeights[rowIndex]
	}
	if rowIndex < len(el.Rows) {
		for _, cell := range el.Rows[rowIndex] {
			if cell.CellHeight > 0 && cell.CellHeight*ptToMM > rowH {
				rowH = cell.CellHeight * ptToMM
			}
		}
	}
	return rowH
}

func drawImageCellFrame(pdf *fpdf.Fpdf, x, y, w, h float64, border string) {
	switch {
	case border == "1":
		pdf.Rect(x, y, w, h, "FD")
	default:
		pdf.Rect(x, y, w, h, "F")
		if border != "" {
			drawCellBorderSides(pdf, x, y, w, h, border)
		}
	}
}

func drawCellBorderSides(pdf *fpdf.Fpdf, x, y, w, h float64, border string) {
	if strings.Contains(border, "L") {
		pdf.Line(x, y, x, y+h)
	}
	if strings.Contains(border, "T") {
		pdf.Line(x, y, x+w, y)
	}
	if strings.Contains(border, "R") {
		pdf.Line(x+w, y, x+w, y+h)
	}
	if strings.Contains(border, "B") {
		pdf.Line(x, y+h, x+w, y+h)
	}
}

func registerCellImage(pdf *fpdf.Fpdf, el *Element, row, col int, cell *CellProps) (string, string, *fpdf.ImageInfoType) {
	imgBytes, imgType, ok := decodeImageDataURL(cell.ImageData)
	if !ok {
		return "", "", nil
	}
	imgName := fmt.Sprintf("cell_%s_%d_%d", el.ID, row, col)
	info := pdf.RegisterImageOptionsReader(imgName, fpdf.ImageOptions{ImageType: imgType}, bytes.NewReader(imgBytes))
	return imgName, imgType, info
}

func drawCellImage(pdf *fpdf.Fpdf, imgName, imgType string, info *fpdf.ImageInfoType, cell *CellProps, x, y, w, h float64) {
	pad := cell.ImagePad * ptToMM
	if pad < 0 {
		pad = 0
	}
	maxPad := minFloat(w, h) / 2
	if pad > maxPad {
		pad = maxPad
	}

	ix := x + pad
	iy := y + pad
	iw := w - 2*pad
	ih := h - 2*pad
	if iw <= 0 || ih <= 0 {
		return
	}

	if strings.ToLower(cell.ImageFit) != "stretch" {
		srcW, srcH := info.Extent()
		if srcW > 0 && srcH > 0 {
			scale := minFloat(iw/srcW, ih/srcH)
			iw = srcW * scale
			ih = srcH * scale
			ix = x + (w-iw)/2
			iy = y + (h-ih)/2
		}
	}

	pdf.ImageOptions(imgName, ix, iy, iw, ih, false, fpdf.ImageOptions{ImageType: imgType}, 0, "")
}

func renderFooter(pdf *fpdf.Fpdf, el *Element, contentW float64) {
	fam := coalesce(el.Font, "Helvetica")
	sz := el.Size
	if sz == 0 {
		sz = 10
	}
	pdf.SetFont(fam, fontStyle(el.Bold, el.Italic, el.Underline), sz)
	setTx(pdf, coalesce(el.TextColor2, "#000000"))
	pdf.SetFillColor(255, 255, 255)

	border := ""
	if el.FooterBorders != nil {
		border = borderStr(el.FooterBorders)
	}
	align := strings.ToUpper(coalesce(el.Align, "C"))
	pdf.CellFormat(contentW, 7, resolvePageVars(pdf, el.Text), border, 1, align, false, 0, "")
}

// renderEventsTable queries event entries and renders them as a bordered table.
func renderEventsTable(ctx context.Context, pdf *fpdf.Fpdf, el *Element, contentW float64, gc GenerateContext) {
	cfg := el.EventsConfig

	end := time.Now()
	start := end.Add(-parseLookback(cfg.Lookback))

	limit := cfg.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	filter := sqldb.EventFilter{
		OrgName:   gc.OrgName,
		Severity:  cfg.Severity,
		Device:    cfg.Device,
		Search:    cfg.Search,
		StartTime: &start,
		EndTime:   &end,
		Limit:     limit,
	}

	entries, err := gc.EventsQueryer(ctx, filter)
	if err != nil {
		log.Printf("[events] query error: %v", err)
		return
	}
	if len(entries) == 0 {
		return
	}

	// Determine visible columns and their labels / relative widths.
	type colDef struct {
		key   string
		label string
		width float64 // relative weight
	}
	allCols := []colDef{
		{"timestamp", "Timestamp", 2.5},
		{"severity", "Severity", 1.2},
		{"user", "User", 1.5},
		{"device", "Device", 1.5},
		{"message", "Message", 4},
		{"params", "Parameters", 2.5},
	}

	cols := cfg.Columns
	if len(cols) == 0 {
		cols = []string{"timestamp", "severity", "device", "message"}
	}
	colSet := make(map[string]bool, len(cols))
	for _, c := range cols {
		colSet[c] = true
	}

	var visible []colDef
	for _, cd := range allCols {
		if colSet[cd.key] {
			visible = append(visible, cd)
		}
	}
	if len(visible) == 0 {
		return
	}

	// Calculate absolute column widths.
	totalW := 0.0
	for _, cd := range visible {
		totalW += cd.width
	}
	colWidths := make([]float64, len(visible))
	for i, cd := range visible {
		colWidths[i] = contentW * cd.width / totalW
	}

	fontSize := el.Size
	if fontSize <= 0 {
		fontSize = 8
	}
	rowH := fontSize*ptToMM*1.8 + 1.0 // comfortable row height

	hdrBg := coalesce(el.BgColor, "#1e3a5f")
	hdrTx := coalesce(el.TextColor, "#ffffff")

	// ── Header row ──
	setBg(pdf, hdrBg)
	setTx(pdf, hdrTx)
	pdf.SetFont("Helvetica", "B", fontSize)
	for i, cd := range visible {
		ln := 0
		if i == len(visible)-1 {
			ln = 1
		}
		pdf.CellFormat(colWidths[i], rowH, cd.label, "1", ln, "L", true, 0, "")
	}

	// ── Data rows ──
	pdf.SetFont("Helvetica", "", fontSize)
	for ri, entry := range entries {
		// Alternating row background
		if ri%2 == 0 {
			setBg(pdf, "#ffffff")
		} else {
			setBg(pdf, "#f5f7fa")
		}
		setTx(pdf, "#000000")

		for i, cd := range visible {
			ln := 0
			if i == len(visible)-1 {
				ln = 1
			}
			val := eventsColValue(entry, cd.key)
			pdf.CellFormat(colWidths[i], rowH, val, "LR", ln, "L", true, 0, "")
		}
	}

	// Bottom border
	setBg(pdf, "#ffffff")
	for i, w := range colWidths {
		ln := 0
		if i == len(colWidths)-1 {
			ln = 1
		}
		pdf.CellFormat(w, 0, "", "T", ln, "", false, 0, "")
	}
}

// eventsColValue extracts a display string for the given column key from an event entry.
func eventsColValue(e events.EventEntry, key string) string {
	switch key {
	case "timestamp":
		return e.Timestamp.Format("2006-01-02 15:04:05")
	case "severity":
		return e.Severity
	case "user":
		if e.UserName != "" {
			return e.UserName
		}
		if e.UserID != nil {
			return fmt.Sprintf("uid:%d", *e.UserID)
		}
		return ""
	case "device":
		return e.Device
	case "message":
		return e.Message
	case "params":
		if len(e.Params) == 0 {
			return ""
		}
		parts := make([]string, 0, len(e.Params))
		for k, v := range e.Params {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func fontStyle(bold, italic, underline bool) string {
	s := ""
	if bold {
		s += "B"
	}
	if italic {
		s += "I"
	}
	if underline {
		s += "U"
	}
	return s
}

func gridBorder(allBorders bool, _ bool, cell *Borders) string {
	if cell != nil {
		return borderStr(cell)
	}
	if allBorders {
		return "1"
	}
	return ""
}

func borderStr(b *Borders) string {
	if b == nil {
		return ""
	}
	s := ""
	if b.Left > 0 {
		s += "L"
	}
	if b.Right > 0 {
		s += "R"
	}
	if b.Top > 0 {
		s += "T"
	}
	if b.Bottom > 0 {
		s += "B"
	}
	return s
}

func setBg(pdf *fpdf.Fpdf, hex string) {
	r, g, b := hexToRGB(hex)
	pdf.SetFillColor(r, g, b)
}

func setTx(pdf *fpdf.Fpdf, hex string) {
	r, g, b := hexToRGB(hex)
	pdf.SetTextColor(r, g, b)
}

func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(hex[0:2], 16, 32)
	g, _ := strconv.ParseInt(hex[2:4], 16, 32)
	b, _ := strconv.ParseInt(hex[4:6], 16, 32)
	return int(r), int(g), int(b)
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func ensureVerticalSpace(pdf *fpdf.Fpdf, heightMM, pageH, bottomMarginMM float64) {
	if heightMM <= 0 {
		return
	}
	_, topMargin, _, _ := pdf.GetMargins()
	currentY := pdf.GetY()
	if currentY <= topMargin+0.1 {
		return
	}
	if currentY+heightMM > pageH-bottomMarginMM {
		pdf.AddPage()
	}
}

func ptOrDefault(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

// resolvePageVars replaces «PAGE_NO» with the current page number just before
// text is rendered (page number is known at this point).
// «PAGE_COUNT» is handled by fpdf.AliasNbPages and resolved at Output() time.
func resolvePageVars(pdf *fpdf.Fpdf, text string) string {
	if strings.Contains(text, "«PAGE_NO»") {
		text = strings.ReplaceAll(text, "«PAGE_NO»", strconv.Itoa(pdf.PageNo()))
	}
	return text
}
