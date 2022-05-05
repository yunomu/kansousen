package kifudb

import "context"

type Record struct {
	KifuId string `dynamodbav:kifuId"`
	SK     string `dynamodbav:"sk"`

	Type string `dynamodbav:"type,omitepty"`

	UserId    string `dynamodbav:"userId,omitepty"`
	Timestamp int64  `dynamodbav:"timestamp,omitepty"`

	CreatedTs int64  `dynamodbav:"createdTs,omitepty"`
	StartTs   int64  `dynamodbav:"startTs,omitepty"`
	EndTs     int64  `dynamodbav:"endTs,omitepty"`
	Handicap  string `dynamodbav:"handicap,omitepty"`
	GameName  string `dynamodbav:"gameName,omitepty"`
	Note      string `dynamodbav:"note,omitepty"`

	OtherName  string `dynamodbav:"otherName,omitepty"`
	OtherValue string `dynamodbav:"otherValue,omitepty"`

	PlayerName  string `dynamodbav:"playerName,omitepty"`
	PlayerOrder string `dynamodbav:"playerOrder,omitepty"`
	PlayerNote  string `dynamodbav:"playerNote,omitepty"`

	Sfen string `dynamodbav:"sfen,omitepty"`

	Seq      int32  `dynamodbav:"seq,omitepty"`
	Move     string `dynamodbav:"move,omitepty"`
	Piece    string `dynamodbav:"piece,omitepty"`
	Finished string `dynamodbav:"finished,omitepty"`
	TimeSec  int64  `dynamodbav:"timeSec,omitepty"`
	ThinkSec int64  `dynamodbav:"thinkSec,omitepty"`

	Position string `dynamodbav:"position,omitepty"`

	StepNoteId   int32  `dynamodbav:"stepNoteId,omitepty"`
	StepNoteText string `dynamodbav:"stepNoteText,omitepty"`
}

type Variant string

const (
	Var_NoValue      Variant = ""
	Var_KifuId       Variant = "kifuId"
	Var_SK           Variant = "sk"
	Var_Type         Variant = "type"
	Var_UserId       Variant = "userId"
	Var_Timestamp    Variant = "timestamp"
	Var_CreatedTs    Variant = "createdTs"
	Var_StartTs      Variant = "startTs"
	Var_EndTs        Variant = "endTs"
	Var_Handicap     Variant = "handicap"
	Var_GameName     Variant = "gameName"
	Var_Note         Variant = "note"
	Var_OtherName    Variant = "otherName"
	Var_OtherValue   Variant = "otherValue"
	Var_PlayerName   Variant = "playerName"
	Var_PlayerOrder  Variant = "playerOrder"
	Var_PlayerNote   Variant = "playerNote"
	Var_Sfen         Variant = "sfen"
	Var_Seq          Variant = "seq"
	Var_Move         Variant = "move"
	Var_Piece        Variant = "piece"
	Var_Finished     Variant = "finished"
	Var_TimeSec      Variant = "timeSec"
	Var_ThinkSec     Variant = "thinkSec"
	Var_Position     Variant = "position"
	Var_StepNoteId   Variant = "stepNoteId"
	Var_StepNoteText Variant = "stepNoteText"
)

var Vars = []Variant{
	Var_KifuId,
	Var_SK,
	Var_Type,
	Var_UserId,
	Var_Timestamp,
	Var_CreatedTs,
	Var_StartTs,
	Var_EndTs,
	Var_Handicap,
	Var_GameName,
	Var_Note,
	Var_OtherName,
	Var_OtherValue,
	Var_PlayerName,
	Var_PlayerOrder,
	Var_PlayerNote,
	Var_Sfen,
	Var_Seq,
	Var_Move,
	Var_Piece,
	Var_Finished,
	Var_TimeSec,
	Var_ThinkSec,
	Var_Position,
	Var_StepNoteId,
	Var_StepNoteText,
}

func (v Variant) isValid() bool {
	for _, v_ := range Vars {
		if v == v_ {
			return true
		}
	}

	return false
}

type Type string

const (
	Type_NoType   Type = ""
	Type_Kifu     Type = "KIFU"
	Type_Player   Type = "PLAYER"
	Type_Step     Type = "STEP"
	Type_SFEN     Type = "SFEN"
	Type_Pos      Type = "POS"
	Type_Other    Type = "OTHER"
	Type_StepNote Type = "STEP_NOTE"
)

var Types = []Type{
	Type_Kifu,
	Type_Player,
	Type_Step,
	Type_SFEN,
	Type_Pos,
	Type_Other,
	Type_StepNote,
}

func (t Type) isValid() bool {
	for _, t_ := range Types {
		if t == t_ {
			return true
		}
	}
	return false
}

type DB interface {
	GetKifu(ctx context.Context, kifuId string, types []Type, vars []Variant) ([]*Record, error)
}
