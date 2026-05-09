package id3v2

// v22To24 maps ID3v2.2 3-character frame IDs to their canonical
// ID3v2.3/2.4 4-character equivalents. IDs absent from the table are
// treated as unknown and round-tripped using the original 3 chars.
var v22To24 = map[string]string{
	"BUF": "RBUF",
	"CNT": "PCNT",
	"COM": "COMM",
	"CRA": "AENC",
	"ETC": "ETCO",
	"EQU": "EQUA",
	"GEO": "GEOB",
	"IPL": "IPLS",
	"LNK": "LINK",
	"MCI": "MCDI",
	"MLL": "MLLT",
	"PIC": "APIC",
	"POP": "POPM",
	"REV": "RVRB",
	"RVA": "RVAD",
	"SLT": "SYLT",
	"STC": "SYTC",
	"TAL": "TALB",
	"TBP": "TBPM",
	"TCM": "TCOM",
	"TCO": "TCON",
	"TCR": "TCOP",
	"TDA": "TDAT",
	"TDY": "TDLY",
	"TEN": "TENC",
	"TFT": "TFLT",
	"TIM": "TIME",
	"TKE": "TKEY",
	"TLA": "TLAN",
	"TLE": "TLEN",
	"TMT": "TMED",
	"TOA": "TOPE",
	"TOF": "TOFN",
	"TOL": "TOLY",
	"TOR": "TORY",
	"TOT": "TOAL",
	"TP1": "TPE1",
	"TP2": "TPE2",
	"TP3": "TPE3",
	"TP4": "TPE4",
	"TPA": "TPOS",
	"TPB": "TPUB",
	"TRC": "TSRC",
	"TRD": "TRDA",
	"TRK": "TRCK",
	"TSI": "TSIZ",
	"TSS": "TSSE",
	"TT1": "TIT1",
	"TT2": "TIT2",
	"TT3": "TIT3",
	"TXT": "TEXT",
	"TXX": "TXXX",
	"TYE": "TYER",
	"UFI": "UFID",
	"ULT": "USLT",
	"WAF": "WOAF",
	"WAR": "WOAR",
	"WAS": "WOAS",
	"WCM": "WCOM",
	"WCP": "WCOP",
	"WPB": "WPUB",
	"WXX": "WXXX",
}

var v24To22 map[string]string

func init() {
	v24To22 = make(map[string]string, len(v22To24))
	for k, v := range v22To24 {
		v24To22[v] = k
	}
}

// canonicalFromV22 returns the 4-character canonical ID for a v2.2
// 3-character ID, or "" if the mapping is unknown. Callers should
// fall back to the raw 3-character ID for round-trip preservation
// when this returns "".
func canonicalFromV22(id string) string {
	return v22To24[id]
}

// v22FromCanonical is the inverse of canonicalFromV22; it returns ""
// for canonical IDs without a v2.2 equivalent.
func v22FromCanonical(id string) string {
	return v24To22[id]
}
