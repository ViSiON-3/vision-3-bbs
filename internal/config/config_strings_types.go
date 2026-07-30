package config

// StringsConfig holds all the configurable strings for the BBS.
// This is the single source of truth.
type StringsConfig struct {
	ConnectionStr           string `json:"connectionStr"`
	LockedBaudStr           string `json:"lockedBaudStr"`
	ApplyAsNewStr           string `json:"applyAsNewStr"`
	GetNupStr               string `json:"getNupStr"`
	ChatRequestStr          string `json:"chatRequestStr"`
	LeaveFBStr              string `json:"leaveFBStr"`
	QuoteTitle              string `json:"quoteTitle"`
	QuoteMessageStr         string `json:"quoteMessageStr"`
	QuoteStartLine          string `json:"quoteStartLine"`
	QuoteEndLine            string `json:"quoteEndLine"`
	Erase5MsgsStr           string `json:"erase5MsgsStr"`
	ChangeBoardStr          string `json:"changeBoardStr"`
	NewscanBoardStr         string `json:"newscanBoardStr"`
	PostOnBoardStr          string `json:"postOnBoardStr"`
	MsgTitleStr             string `json:"msgTitleStr"`
	MsgToStr                string `json:"msgToStr"`
	MsgAnonStr              string `json:"msgAnonStr"`
	AnonymousName           string `json:"anonymousName"`
	SlashStr                string `json:"slashStr"`
	NewScanningStr          string `json:"newScanningStr"`
	ChangeFileAreaStr       string `json:"changeFileAreaStr"`
	LogOffStr               string `json:"logOffStr"`
	ChangeAutoMsgStr        string `json:"changeAutoMsgStr"`
	NewUserNameStr          string `json:"newUserNameStr"`
	CreateAPassword         string `json:"createAPassword"`
	PauseString             string `json:"pauseString"`
	WhatsYourAlias          string `json:"whatsYourAlias"`
	WhatsYourPw             string `json:"whatsYourPw"`
	SysopWorkingStr         string `json:"sysopWorkingStr"`
	SysopInDos              string `json:"sysopInDos"`
	SystemPasswordStr       string `json:"systemPasswordStr"`
	DefPrompt               string `json:"defPrompt"`
	EnterChat               string `json:"enterChat"`
	ExitChat                string `json:"exitChat"`
	SysOpIsIn               string `json:"sysOpIsIn"`
	SysOpIsOut              string `json:"sysOpIsOut"`
	HeaderStr               string `json:"headerStr"`
	InfoformPrompt          string `json:"infoformPrompt"`
	NewInfoFormPrompt       string `json:"newInfoFormPrompt"`
	UserNotFound            string `json:"userNotFound"`
	DesignNewPrompt         string `json:"designNewPrompt"`
	YourCurrentPrompt       string `json:"yourCurrentPrompt"`
	WantHotKeys             string `json:"wantHotKeys"`
	WantRumors              string `json:"wantRumors"`
	YourUserNum             string `json:"yourUserNum"`
	WelcomeNewUser          string `json:"welcomeNewUser"`
	EnterNumberHeader       string `json:"enterNumberHeader"`
	EnterNumber             string `json:"enterNumber"`
	EnterUserNote           string `json:"enterUserNote"`
	CurFileArea             string `json:"curFileArea"`
	EnterRealName           string `json:"enterRealName"`
	ReEnterPassword         string `json:"reEnterPassword"`
	QuoteTop                string `json:"quoteTop"`
	QuoteBottom             string `json:"quoteBottom"`
	AskOneLiner             string `json:"askOneLiner"`
	OneLinerAnonymousPrompt string `json:"oneLinerAnonymousPrompt"`
	EnterOneLiner           string `json:"enterOneLiner"`
	OneLinerLegend          string `json:"One_Liner_Legend"`
	NewScanDateStr          string `json:"newScanDateStr"`
	AddBatchPrompt          string `json:"addBatchPrompt"`
	ListUsers               string `json:"listUsers"`
	ViewArchivePrompt       string `json:"viewArchivePrompt"`
	AreaMsgNewScan          string `json:"areaMsgNewScan"`
	GetInfoPrompt           string `json:"getInfoPrompt"`
	MsgNewScanPrompt        string `json:"msgNewScanPrompt"`
	TypeFilePrompt          string `json:"typeFilePrompt"`
	ConfPrompt              string `json:"confPrompt"`
	FileListPrompt          string `json:"fileListPrompt"`
	UploadFileStr           string `json:"uploadFileStr"`
	DownloadStr             string `json:"downloadStr"`
	ListRange               string `json:"listRange"`
	ContinueStr             string `json:"continueStr"`
	ViewWhichForm           string `json:"viewWhichForm"`
	CheckingUserBase        string `json:"checkingUserBase"`
	NameAlreadyUsed         string `json:"nameAlreadyUsed"`
	InvalidUserName         string `json:"invalidUserName"`
	SysPwIs                 string `json:"sysPwIs"`
	NotValidated            string `json:"notValidated"`
	HaveMail                string `json:"haveMail"`
	ReadMailNow             string `json:"readMailNow"`
	DeleteNotice            string `json:"deleteNotice"`
	HaveFeedback            string `json:"haveFeedback"`
	ReadFeedback            string `json:"readFeedback"`
	LoginNow                string `json:"loginNow"`
	NewUsersWaiting         string `json:"newUsersWaiting"`
	VoteOnNewUsers          string `json:"voteOnNewUsers"`
	WrongPassword           string `json:"wrongPassword"`
	YesPromptText           string `json:"yesPromptText"`
	NoPromptText            string `json:"noPromptText"`
	AbortMessagePrompt      string `json:"abortMessagePrompt"`

	// Added from Page 5
	AddBBSName          string `json:"addBBSName"`
	AddBBSNumber        string `json:"addBBSNumber"`
	AddBBSBaud          string `json:"addBBSBaud"`
	AddBBSSoftware      string `json:"addBBSSoftware"`
	AddExtendedBBSDescr string `json:"addExtendedBBSDescr"`
	BBSEntryAdded       string `json:"bbsEntryAdded"`
	ViewNextDescrip     string `json:"viewNextDescrip"`
	JoinedMsgConf       string `json:"joinedMsgConf"`
	JoinedFileConf      string `json:"joinedFileConf"`
	WhosBeingVotedOn    string `json:"whosBeingVotedOn"`
	NumYesVotes         string `json:"numYesVotes"`
	NumNoVotes          string `json:"numNoVotes"`
	NUVCommentHeader    string `json:"nuvCommentHeader"`

	// Added from Page 6
	EnterNUVCommentPrompt string `json:"enterNUVCommentPrompt"`
	NUVVotePrompt         string `json:"nuvVotePrompt"`
	YesVoteCast           string `json:"yesVoteCast"`
	NoVoteCast            string `json:"noVoteCast"`
	NoNewUsersPending     string `json:"noNewUsersPending"`
	EnterRumorTitle       string `json:"enterRumorTitle"`
	AddRumorAnonymous     string `json:"addRumorAnonymous"`
	EnterRumorLevel       string `json:"enterRumorLevel"`
	EnterRumorPrompt      string `json:"enterRumorPrompt"`
	RumorAdded            string `json:"rumorAdded"`
	ListRumorsPrompt      string `json:"listRumorsPrompt"`
	SendMailToWho         string `json:"sendMailToWho"`
	CarbonCopyMail        string `json:"carbonCopyMail"`
	NotifyEMail           string `json:"notifyEMail"`
	EMailAnnouncement     string `json:"eMailAnnouncement"`
	SysOpNotHere          string `json:"sysOpNotHere"`
	ChatCostsHeader       string `json:"chatCostsHeader"`
	StillWantToTry        string `json:"stillWantToTry"`
	NotEnoughFPPoints     string `json:"notEnoughFPPoints"`
	ChatCallOff           string `json:"chatCallOff"`

	// Added from Page 7
	ChatCallOn          string `json:"chatCallOn"`
	FeedbackSent        string `json:"feedbackSent"`
	YouHaveReadMail     string `json:"youHaveReadMail"`
	DeleteMailNow       string `json:"deleteMailNow"`
	CurrentMailNone     string `json:"currentMailNone"`
	CurrentMailWaiting  string `json:"currentMailWaiting"`
	PickMailHeader      string `json:"pickMailHeader"`
	ListTitleType       string `json:"listTitleType"`
	NoMoreTitles        string `json:"noMoreTitles"`
	ListTitlesToYou     string `json:"listTitlesToYou"`
	SubDoesNotExist     string `json:"subDoesNotExist"`
	MsgNewScanAborted   string `json:"msgNewScanAborted"`
	MsgReadingPrompt    string `json:"msgReadingPrompt"`
	CurrentSubNewScan   string `json:"currentSubNewScan"`
	JumpToMessageNum    string `json:"jumpToMessageNum"`
	PostingQWKMsg       string `json:"postingQWKMsg"`
	TotalQWKAdded       string `json:"totalQWKAdded"`
	SendQWKPacketPrompt string `json:"sendQWKPacketPrompt"`

	// Added from Page 8
	ThreadWhichWay       string `json:"threadWhichWay"`
	AutoValidatingFile   string `json:"autoValidatingFile"`
	FileIsWorth          string `json:"fileIsWorth"`
	GrantingUserFP       string `json:"grantingUserFP"`
	FileIsOffline        string `json:"fileIsOffline"`
	CrashedFile          string `json:"crashedFile"`
	BadBaudRate          string `json:"badBaudRate"`
	UnvalidatedFile      string `json:"unvalidatedFile"`
	SpecialFile          string `json:"specialFile"`
	NoDownloadsHere      string `json:"noDownloadsHere"`
	PrivateFile          string `json:"privateFile"`
	FilePassword         string `json:"filePassword"`
	WrongFilePW          string `json:"wrongFilePW"`
	FileNewScanPrompt    string `json:"fileNewScanPrompt"`
	InvalidArea          string `json:"invalidArea"`
	UntaggingBatchFile   string `json:"untaggingBatchFile"`
	FileExtractionPrompt string `json:"fileExtractionPrompt"`

	// Added from Page 9
	BadUDRatio             string `json:"badUDRatio"`
	BadUDKRatio            string `json:"badUDKRatio"`
	ExceededDailyKBLimit   string `json:"exceededDailyKBLimit"`
	FilePointCommision     string `json:"filePointCommision"`
	SuccessfulDownload     string `json:"successfulDownload"`
	FileCrashSave          string `json:"fileCrashSave"`
	InvalidFilename        string `json:"invalidFilename"`
	AlreadyEnteredFilename string `json:"alreadyEnteredFilename"`
	FileAlreadyExists      string `json:"fileAlreadyExists"`
	EnterFileDescription   string `json:"enterFileDescription"`
	ExtendedUploadSetup    string `json:"extendedUploadSetup"`
	ReEnterFileDescrip     string `json:"reEnterFileDescrip"`
	NotifyIfDownloaded     string `json:"notifyIfDownloaded"`
	FiftyFilesMaximum      string `json:"fiftyFilesMaximum"`
	YouCantDownloadHere    string `json:"youCantDownloadHere"`
	FileAlreadyMarked      string `json:"fileAlreadyMarked"`
	NotEnoughFP            string `json:"notEnoughFP"`
	FileAreaPassword       string `json:"fileAreaPassword"`

	// Download/batch workflow strings (V3-specific)
	BatchQueueEmpty        string `json:"batchQueueEmpty"`
	BatchClearedFormat     string `json:"batchClearedFormat"`
	AddedToBatchFormat     string `json:"addedToBatchFormat"`
	NoFilesTagged          string `json:"noFilesTagged"`
	BatchCountFormat       string `json:"batchCountFormat"`
	FilesResolveError      string `json:"filesResolveError"`
	DownloadFinishedFormat string `json:"downloadFinishedFormat"`
	FileAreaNotFound       string `json:"fileAreaNotFound"`
	SaveUserError          string `json:"saveUserError"`

	QuotePrefix string `json:"QuotePrefix"`

	// Chat strings (V3-specific)
	ChatHeader        string `json:"chatHeader"`
	ChatSeparator     string `json:"chatSeparator"`
	ChatUserEntered   string `json:"chatUserEntered"`
	ChatUserLeft      string `json:"chatUserLeft"`
	ChatSystemPrefix  string `json:"chatSystemPrefix"`
	ChatMessageFormat string `json:"chatMessageFormat"`

	ChatNetworkPickerHeader string `json:"chatNetworkPickerHeader"`
	ChatNetworkPickerEntry  string `json:"chatNetworkPickerEntry"`
	ChatRoomListHeader      string `json:"chatRoomListHeader"`
	ChatRoomListEntry       string `json:"chatRoomListEntry"`
	ChatPrivateMsgFormat    string `json:"chatPrivateMsgFormat"`
	ChatJoinMsg             string `json:"chatJoinMsg"`
	ChatLeaveMsg            string `json:"chatLeaveMsg"`
	ChatTopicMsg            string `json:"chatTopicMsg"`
	ChatReconnected         string `json:"chatReconnected"`

	// Page strings (V3-specific)
	PageOnlineNodesHeader string `json:"pageOnlineNodesHeader"`
	PageNodeListEntry     string `json:"pageNodeListEntry"`
	PageWhichNodePrompt   string `json:"pageWhichNodePrompt"`
	PageMessagePrompt     string `json:"pageMessagePrompt"`
	PageMessageFormat     string `json:"pageMessageFormat"`
	PageSent              string `json:"pageSent"`
	PageCancelled         string `json:"pageCancelled"`
	PageInvalidNode       string `json:"pageInvalidNode"`
	PageSelfError         string `json:"pageSelfError"`
	PageNodeOffline       string `json:"pageNodeOffline"`

	// Newuser strings (V3-specific)
	NewUsersClosedStr       string `json:"newUsersClosedStr"`
	NewUserLocationPrompt   string `json:"newUserLocationPrompt"`
	NewUserPasswordTooShort string `json:"newUserPasswordTooShort"`
	NewUserPasswordMismatch string `json:"newUserPasswordMismatch"`
	NewUserInvalidRealName  string `json:"newUserInvalidRealName"`
	NewUserTooManyAttempts  string `json:"newUserTooManyAttempts"`
	NewUserAccountCreated   string `json:"newUserAccountCreated"`
	NewUserCreationError    string `json:"newUserCreationError"`
	NewUserMaybeAnotherTime string `json:"newUserMaybeAnotherTime"`

	// System stats strings (V3-specific)
	StatsBBSName     string `json:"statsBBSName"`
	StatsSysOp       string `json:"statsSysOp"`
	StatsVersion     string `json:"statsVersion"`
	StatsTotalUsers  string `json:"statsTotalUsers"`
	StatsTotalCalls  string `json:"statsTotalCalls"`
	StatsTotalMsgs   string `json:"statsTotalMessages"`
	StatsTotalFiles  string `json:"statsTotalFiles"`
	StatsActiveNodes string `json:"statsActiveNodes"`
	StatsDate        string `json:"statsDate"`
	StatsTime        string `json:"statsTime"`

	// User config strings (V3-specific)
	CfgToggleOn            string `json:"cfgToggleOn"`
	CfgToggleOff           string `json:"cfgToggleOff"`
	CfgToggleFormat        string `json:"cfgToggleFormat"`
	CfgSaveError           string `json:"cfgSaveError"`
	CfgScreenWidthPrompt   string `json:"cfgScreenWidthPrompt"`
	CfgScreenWidthInvalid  string `json:"cfgScreenWidthInvalid"`
	CfgScreenWidthSet      string `json:"cfgScreenWidthSet"`
	CfgScreenHeightPrompt  string `json:"cfgScreenHeightPrompt"`
	CfgScreenHeightInvalid string `json:"cfgScreenHeightInvalid"`
	CfgScreenHeightSet     string `json:"cfgScreenHeightSet"`
	CfgTermTypeSet         string `json:"cfgTermTypeSet"`
	CfgStringPrompt        string `json:"cfgStringPrompt"`
	CfgStringPromptCurrent string `json:"cfgStringPromptCurrent"`
	CfgStringUpdated       string `json:"cfgStringUpdated"`
	CfgCurrentPwPrompt     string `json:"cfgCurrentPwPrompt"`
	CfgIncorrectPw         string `json:"cfgIncorrectPw"`
	CfgPasswordChanged     string `json:"cfgPasswordChanged"`
	CfgColorSelectPrompt   string `json:"cfgColorSelectPrompt"`
	CfgColorInputPrompt    string `json:"cfgColorInputPrompt"`
	CfgColorInvalid        string `json:"cfgColorInvalid"`
	CfgColorSet            string `json:"cfgColorSet"`
	CfgCustomPromptHelp    string `json:"cfgCustomPromptHelp"`
	CfgViewScreenWidth     string `json:"cfgViewScreenWidth"`
	CfgViewScreenHeight    string `json:"cfgViewScreenHeight"`
	CfgViewTermType        string `json:"cfgViewTermType"`
	CfgViewHotKeys         string `json:"cfgViewHotKeys"`
	CfgViewMorePrompts     string `json:"cfgViewMorePrompts"`
	CfgViewMsgHeader       string `json:"cfgViewMsgHeader"`
	CfgViewCustomPrompt    string `json:"cfgViewCustomPrompt"`
	CfgViewPromptColor     string `json:"cfgViewPromptColor"`
	CfgViewTextColor       string `json:"cfgViewTextColor"`
	CfgViewText2Color      string `json:"cfgViewText2Color"`
	CfgViewBarColor        string `json:"cfgViewBarColor"`
	CfgViewRealName        string `json:"cfgViewRealName"`
	CfgViewNote            string `json:"cfgViewNote"`
	CfgViewFileListMode    string `json:"cfgViewFileListMode"`
	CfgFileListModeSet     string `json:"cfgFileListModeSet"`

	// Message reader strings (V3-specific)
	MsgEndOfMessages     string `json:"msgEndOfMessages"`
	MsgFirstMessage      string `json:"msgFirstMessage"`
	MsgMailReplyDeferred string `json:"msgMailReplyDeferred"`
	MsgListDeferred      string `json:"msgListDeferred"`
	MsgHdrLoadError      string `json:"msgHdrLoadError"`
	MsgBoardInfoFormat   string `json:"msgBoardInfoFormat"`
	MsgScrollPercent     string `json:"msgScrollPercent"`
	MsgNewScanSuffix     string `json:"msgNewScanSuffix"`
	MsgReadingSuffix     string `json:"msgReadingSuffix"`
	MsgThreadPrompt      string `json:"msgThreadPrompt"`
	MsgNoThreadFound     string `json:"msgNoThreadFound"`
	MsgJumpPrompt        string `json:"msgJumpPrompt"`
	MsgInvalidMsgNum     string `json:"msgInvalidMsgNum"`
	MsgReplySubjectEmpty string `json:"msgReplySubjectEmpty"`
	MsgLaunchingEditor   string `json:"msgLaunchingEditor"`
	MsgReplyCancelled    string `json:"msgReplyCancelled"`
	MsgReplyError        string `json:"msgReplyError"`
	MsgReplySuccess      string `json:"msgReplySuccess"`
	MsgEditorError       string `json:"msgEditorError"`

	// Message scan strings (V3-specific)
	ScanDateLine            string `json:"scanDateLine"`
	ScanToLine              string `json:"scanToLine"`
	ScanFromLine            string `json:"scanFromLine"`
	ScanRangeLine           string `json:"scanRangeLine"`
	ScanUpdateLine          string `json:"scanUpdateLine"`
	ScanWhichLine           string `json:"scanWhichLine"`
	ScanAbortLine           string `json:"scanAbortLine"`
	ScanSelectionPrompt     string `json:"scanSelectionPrompt"`
	ScanDatePrompt          string `json:"scanDatePrompt"`
	ScanToPrompt            string `json:"scanToPrompt"`
	ScanFromPrompt          string `json:"scanFromPrompt"`
	ScanRangeStartPrompt    string `json:"scanRangeStartPrompt"`
	ScanRangeEndPrompt      string `json:"scanRangeEndPrompt"`
	ScanWhichPrompt         string `json:"scanWhichPrompt"`
	ScanHeader              string `json:"scanHeader"`
	ScanNoAreaSelected      string `json:"scanNoAreaSelected"`
	ScanNoMessages          string `json:"scanNoMessages"`
	ScanNoTaggedAreas       string `json:"scanNoTaggedAreas"`
	NewscanNewNetworkPrompt string `json:"newscanNewNetworkPrompt"`
	ScanAreaProgress        string `json:"scanAreaProgress"`
	ScanComplete            string `json:"scanComplete"`
	ScanLoginRequired       string `json:"scanLoginRequired"`
	ScanNoAreasAvailable    string `json:"scanNoAreasAvailable"`
	ScanNoAccessibleAreas   string `json:"scanNoAccessibleAreas"`
	ScanConfigSaved         string `json:"scanConfigSaved"`
	ScanConfigError         string `json:"scanConfigError"`
	ScanConfigLoginRequired string `json:"scanConfigLoginRequired"`

	// Update newscan pointers strings
	UpdatePtrsLoginRequired string `json:"updatePtrsLoginRequired"`
	UpdatePtrsCancelled     string `json:"updatePtrsCancelled"`
	UpdatePtrsDatePrompt    string `json:"updatePtrsDatePrompt"`
	UpdatePtrsScopePrompt   string `json:"updatePtrsScopePrompt"`
	UpdatePtrsSuccess       string `json:"updatePtrsSuccess"`
	UpdatePtrsError         string `json:"updatePtrsError"`

	// Message list strings (V3-specific)
	MsgListLoginRequired  string `json:"msgListLoginRequired"`
	MsgListNoAreaSelected string `json:"msgListNoAreaSelected"`
	MsgListAreaNotFound   string `json:"msgListAreaNotFound"`
	MsgListLoadError      string `json:"msgListLoadError"`
	MsgListNoMessages     string `json:"msgListNoMessages"`

	// File viewer strings (V3-specific)
	FileNoAreaSelected string `json:"fileNoAreaSelected"`
	FilePromptFormat   string `json:"filePromptFormat"`
	FileNotFoundFormat string `json:"fileNotFoundFormat"`
	FileLocateError    string `json:"fileLocateError"`
	FileViewingHeader  string `json:"fileViewingHeader"`
	FileEndOfFile      string `json:"fileEndOfFile"`
	FileMorePrompt     string `json:"fileMorePrompt"`
	FilePausePrompt    string `json:"filePausePrompt"`
	FileOpenError      string `json:"fileOpenError"`

	// File search strings
	SearchFilesPrompt    string `json:"searchFilesPrompt"`
	SearchFilesMinChars  string `json:"searchFilesMinChars"`
	SearchNoResults      string `json:"searchNoResults"`
	SearchResultsHeader  string `json:"searchResultsHeader"`
	SearchResultFormat   string `json:"searchResultFormat"`
	SearchResultsSummary string `json:"searchResultsSummary"`

	// File info strings
	FileInfoPrompt string `json:"fileInfoPrompt"`
	FileInfoHeader string `json:"fileInfoHeader"`

	// File newscan strings
	FileNewscanHeader   string `json:"fileNewscanHeader"`
	FileNewscanAreaHdr  string `json:"fileNewscanAreaHdr"`
	FileNewscanNoNew    string `json:"fileNewscanNoNew"`
	FileNewscanComplete string `json:"fileNewscanComplete"`

	// Sysop file review strings
	SysopReviewHeader  string `json:"sysopReviewHeader"`
	SysopReviewPrompt  string `json:"sysopReviewPrompt"`
	SysopReviewMarked  string `json:"sysopReviewMarked"`
	SysopReviewNoFiles string `json:"sysopReviewNoFiles"`
	SysopReviewScanAll string `json:"sysopReviewScanAll"`
	SysopReviewRenamed string `json:"sysopReviewRenamed"`

	// File newscan config strings
	FileNewscanConfigHeader string `json:"fileNewscanConfigHeader"`
	FileNewscanConfigPrompt string `json:"fileNewscanConfigPrompt"`
	FileNewscanConfigSaved  string `json:"fileNewscanConfigSaved"`

	// Column config strings
	CfgFileColumnsHeader string `json:"cfgFileColumnsHeader"`
	CfgFileColumnsToggle string `json:"cfgFileColumnsToggle"`
	CfgFileColumnsSaved  string `json:"cfgFileColumnsSaved"`

	// Want list strings
	WantListPrompt       string `json:"wantListPrompt"`
	WantListReasonPrompt string `json:"wantListReasonPrompt"`
	WantListSubmitted    string `json:"wantListSubmitted"`
	WantListEmpty        string `json:"wantListEmpty"`
	WantListHeader       string `json:"wantListHeader"`
	WantListCleared      string `json:"wantListCleared"`

	// Door handler strings (V3-specific)
	DoorDropfileError     string `json:"doorDropfileError"`
	DoorErrorFormat       string `json:"doorErrorFormat"`
	DoorLoginRequired     string `json:"doorLoginRequired"`
	DoorPrompt            string `json:"doorPrompt"`
	DoorNotFoundFormat    string `json:"doorNotFoundFormat"`
	DoorNoneConfigured    string `json:"doorNoneConfigured"`
	DoorTemplateError     string `json:"doorTemplateError"`
	DoorInfoLoginRequired string `json:"doorInfoLoginRequired"`
	DoorAccessDenied      string `json:"doorAccessDenied"`
	DoorBusyFormat        string `json:"doorBusyFormat"`

	// Matrix strings (V3-specific)
	MatrixDisconnecting       string `json:"matrixDisconnecting"`
	MatrixCheckAccessPrompt   string `json:"matrixCheckAccessPrompt"`
	MatrixUserNotFound        string `json:"matrixUserNotFound"`
	MatrixAccountValidated    string `json:"matrixAccountValidated"`
	MatrixAccountNotValidated string `json:"matrixAccountNotValidated"`
	IdleTimeout               string `json:"idleTimeout"`

	// Conference menu strings (V3-specific)
	ConfLoginRequired           string `json:"confLoginRequired"`
	ConfNoConferences           string `json:"confNoConferences"`
	ConfNotFound                string `json:"confNotFound"`
	ConfNoAccessibleAreas       string `json:"confNoAccessibleAreas"`
	ConfCurrentAreaFormat       string `json:"confCurrentAreaFormat"`
	ConfTemplateError           string `json:"confTemplateError"`
	ConfNavLoginRequired        string `json:"confNavLoginRequired"`
	ConfNoAccessibleConferences string `json:"confNoAccessibleConferences"`
	ConfNoAccessibleMsgAreas    string `json:"confNoAccessibleMsgAreas"`
	ConfAreaTemplateError       string `json:"confAreaTemplateError"`
	ConfCurrentConfFormat       string `json:"confCurrentConfFormat"`
	ConfNoAccessibleConfs       string `json:"confNoAccessibleConfs"`

	// Executor strings (V3-specific)
	ExecUnknownCommand      string `json:"execUnknownCommand"`
	ExecReadmailLogin       string `json:"execReadmailLogin"`
	ExecReadmailPlaceholder string `json:"execReadmailPlaceholder"`
	ExecDoorLogin           string `json:"execDoorLogin"`
	ExecDoorNotConfigured   string `json:"execDoorNotConfigured"`
	ExecGoodbye             string `json:"execGoodbye"`
	ExecStatsLogin          string `json:"execStatsLogin"`
	ExecStatsError          string `json:"execStatsError"`
	ExecOnelinerTemplateErr string `json:"execOnelinerTemplateErr"`
	ExecOnelinerColorError  string `json:"execOnelinerColorError"`
	ExecOnelinerWriteError  string `json:"execOnelinerWriteError"`
	ExecOnelinerAdded       string `json:"execOnelinerAdded"`
	ExecOnelinerEmpty       string `json:"execOnelinerEmpty"`
	ExecAlreadyLoggedIn     string `json:"execAlreadyLoggedIn"`
	ExecUsernamePrompt      string `json:"execUsernamePrompt"`
	ExecPasswordPrompt      string `json:"execPasswordPrompt"`
	ExecAbortLoginPrompt    string `json:"execAbortLoginPrompt"`
	ExecLoginCancelled      string `json:"execLoginCancelled"`
	ExecIPLockout           string `json:"execIPLockout"`
	ExecLoginIncorrect      string `json:"execLoginIncorrect"`
	ExecNotValidated        string `json:"execNotValidated"`
	ExecMenuLoadError       string `json:"execMenuLoadError"`
	ExecMenuPasswordPrompt  string `json:"execMenuPasswordPrompt"`
	ExecPasswordAccepted    string `json:"execPasswordAccepted"`
	ExecIncorrectPassword   string `json:"execIncorrectPassword"`
	ExecTooManyAttempts     string `json:"execTooManyAttempts"`
	ExecAccessDenied        string `json:"execAccessDenied"`
	ExecLastcallTemplateErr string `json:"execLastcallTemplateErr"`
	ExecFileLoadError       string `json:"execFileLoadError"`
	ExecNoNewMail           string `json:"execNoNewMail"`
	ExecNewMailCount        string `json:"execNewMailCount"`
	ExecUserlistTemplateErr string `json:"execUserlistTemplateErr"`
	ExecPendingValidation   string `json:"execPendingValidation"`
	ExecRunCommandError     string `json:"execRunCommandError"`
	ExecRunCommandNotFound  string `json:"execRunCommandNotFound"`
	ExecRunDoorError        string `json:"execRunDoorError"`
	ExecLoginCriticalError  string `json:"execLoginCriticalError"`
	ExecVersionString       string `json:"execVersionString"`

	// Terminal size mismatch prompts (shown post-login when detected size differs from saved)
	TermSizeNewDetectedPrompt    string `json:"termSizeNewDetectedPrompt"`
	TermSizeUpdateDefaultsPrompt string `json:"termSizeUpdateDefaultsPrompt"`

	// Invisible login prompt (shown to SysOp/CoSysOp after authentication)
	InvisibleLogonPrompt string `json:"invisibleLogonPrompt"`

	// Default Colors (|C1 - |C7 map to these)
	DefColor1 uint8 `json:"defColor1"`
	DefColor2 uint8 `json:"defColor2"`
	DefColor3 uint8 `json:"defColor3"`
	DefColor4 uint8 `json:"defColor4"`
	DefColor5 uint8 `json:"defColor5"`
	DefColor6 uint8 `json:"defColor6"`
	DefColor7 uint8 `json:"defColor7"`
}

// GlobalStrings holds the loaded configuration.
// var GlobalStrings StringsConfig // Removed duplicate
