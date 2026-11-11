package user

// CentralAlertPrefs are a caller's V3Net Central activity alert choices,
// one pair of switches per kind of alert (logons, logoffs, chat joins and
// posts). A Share switch lets other boards be told when this caller does
// that here; a See switch shows this caller the other boards' alerts of
// that kind. The two are separate: asking to see alerts shares nothing,
// and sharing subscribes to nothing. Each is off until the caller turns it
// on, and only takes effect on networks where the SysOp has that kind of
// alert on. Kinds are named as the protocol names them: "logon", "logoff",
// "chat_join" and "post".
type CentralAlertPrefs struct {
	ShareLogons  bool `json:"shareLogons,omitempty"`
	ShareLogoffs bool `json:"shareLogoffs,omitempty"`
	ShareChat    bool `json:"shareChat,omitempty"`
	SharePosts   bool `json:"sharePosts,omitempty"`
	SeeLogons    bool `json:"seeLogons,omitempty"`
	SeeLogoffs   bool `json:"seeLogoffs,omitempty"`
	SeeChat      bool `json:"seeChat,omitempty"`
	SeePosts     bool `json:"seePosts,omitempty"`
}

// Alert kinds, as CentralAlertPrefs takes them.
const (
	CentralAlertLogon    = "logon"
	CentralAlertLogoff   = "logoff"
	CentralAlertChatJoin = "chat_join"
	CentralAlertPost     = "post"
)

// switches returns the Share and See switches for kind, or nils for a kind
// it does not know.
func (p *CentralAlertPrefs) switches(kind string) (share, see *bool) {
	switch kind {
	case CentralAlertLogon:
		return &p.ShareLogons, &p.SeeLogons
	case CentralAlertLogoff:
		return &p.ShareLogoffs, &p.SeeLogoffs
	case CentralAlertChatJoin:
		return &p.ShareChat, &p.SeeChat
	case CentralAlertPost:
		return &p.SharePosts, &p.SeePosts
	}
	return nil, nil
}

// Shares reports whether the caller lets other boards be told of kind.
func (p CentralAlertPrefs) Shares(kind string) bool {
	share, _ := p.switches(kind)
	return share != nil && *share
}

// Sees reports whether the caller asked to see other boards' alerts of kind.
func (p CentralAlertPrefs) Sees(kind string) bool {
	_, see := p.switches(kind)
	return see != nil && *see
}

// SetShares sets the caller's Share switch for kind.
func (p *CentralAlertPrefs) SetShares(kind string, on bool) {
	if share, _ := p.switches(kind); share != nil {
		*share = on
	}
}

// SetSees sets the caller's See switch for kind.
func (p *CentralAlertPrefs) SetSees(kind string, on bool) {
	if _, see := p.switches(kind); see != nil {
		*see = on
	}
}
