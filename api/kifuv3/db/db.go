package db

import (
	"context"
	"errors"

	documentpb "github.com/yunomu/kansousen/proto/document"
)

var (
	ErrUserIdIsEmpty   = errors.New("user_id is empty")
	ErrKifuIdIsEmpty   = errors.New("kifu_id is empty")
	ErrPositionIsEmpty = errors.New("position is empty")
	ErrLockError       = errors.New("optimistic locking error")
)

type searchOptions struct {
	count   int
	stepNum int

	pos            string
	userIds        []string
	excludeKifuIds []string
	sfen           string
}

func defaultSearchOpitons() *searchOptions {
	return &searchOptions{
		count:   10,
		stepNum: 5,
	}
}

type SearchOption func(*searchOptions)

func SetSearchCount(count int) SearchOption {
	return func(o *searchOptions) {
		o.count = count
	}
}

func SetSearchStepNum(stepNum int) SearchOption {
	return func(o *searchOptions) {
		o.stepNum = stepNum
	}
}

func SetSearchPos(pos string) SearchOption {
	return func(o *searchOptions) {
		o.pos = pos
	}
}

func SetSearchUserIds(userIds []string) SearchOption {
	return func(o *searchOptions) {
		o.userIds = userIds
	}
}

func SetSearchExcludeKifuIds(excludeKifuIds []string) SearchOption {
	return func(o *searchOptions) {
		o.excludeKifuIds = excludeKifuIds
	}
}

func SetSearchSfen(sfen string) SearchOption {
	return func(o *searchOptions) {
		o.sfen = sfen
	}
}

type SearchItem struct {
	KifuId string
	UserId string
	Seq    int32
}

type Index interface {
	Search(context.Context, func([]*SearchItem) bool, ...SearchOption) error
}

type getOptions struct {
	offset int
	length int
}

func defaultGetOptions() *getOptions {
	return &getOptions{
		offset: 0,
		length: -1,
	}
}

type GetOption func(*getOptions)

type Handicap string

const (
	Handicap_NONE        Handicap = "NONE"
	Handicap_DROP_L      Handicap = "DROP_L"
	Handicap_DROP_L_R    Handicap = "DROP_L_R"
	Handicap_DROP_B      Handicap = "DROP_B"
	Handicap_DROP_R      Handicap = "DROP_R"
	Handicap_DROP_RL     Handicap = "DROP_RL"
	Handicap_DROP_TWO    Handicap = "DROP_TWO"
	Handicap_DROP_THREE  Handicap = "DROP_THREE"
	Handicap_DROP_FOUR   Handicap = "DROP_FOUR"
	Handicap_DROP_FIVE   Handicap = "DROP_FIVE"
	Handicap_DROP_FIVE_L Handicap = "DROP_FIVE_L"
	Handicap_DROP_SIX    Handicap = "DROP_SIX"
	Handicap_DROP_EIGHT  Handicap = "DROP_EIGHT"
	Handicap_DROP_TEN    Handicap = "DROP_TEN"
	Handicap_OTHER       Handicap = "OTHER"
)

type Kifu struct {
	KifuId    string
	Timestamp int64

	UserId    string
	CreatedTs int64
	StartTs   int64
	EndTs     int64
	Handicap  Handicap
	GameName  string
}

type Order string

const (
	Order_BLACK Order = "BLACK"
	Order_WHITE Order = "WHITE"
)

type Player struct {
	KifuId    string
	Timestamp int64
	Order     Order
	Name      string

	Note string
}

type OtherField struct {
	KifuId    string
	Timestamp int64
	Name      string

	Value string
}

type Piece string

const (
	Piece_NULL      = ""
	Piece_GYOKU     = "GYOKU"
	Piece_HISHA     = "HISHA"
	Piece_RYU       = "RYU"
	Piece_KAKU      = "KAKU"
	Piece_UMA       = "UMA"
	Piece_KIN       = "KIN"
	Piece_GIN       = "GIN"
	Piece_NARI_GIN  = "NARI_GIN"
	Piece_KEI       = "KEI"
	Piece_NARI_KEI  = "NARI_KEI"
	Piece_KYOU      = "KYOU"
	Piece_NARI_KYOU = "NARI_KYOU"
	Piece_FU        = "FU"
	Piece_TO        = "TO"
)

type Finished string

const (
	Finished_NON_FINISHED    Finished = ""
	Finished_SUSPEND         Finished = "SUSPEND"
	Finished_SURRENDER       Finished = "SURRENDER"
	Finished_DRAW            Finished = "DRAW"
	Finished_REPETITION_DRAW Finished = "REPETITION_DRAW"
	Finished_CHECKMATE       Finished = "CHECKMATE"
	Finished_OVER_TIME_LIMIT Finished = "OVER_TIME_LIMIT"
	Finished_FOUL_LOSS       Finished = "FOUL_LOSS"
	Finished_FOUL_WIN        Finished = "FOUL_WIN"
	Finished_NYUGYOKU_WIN    Finished = "NYUGYOKU_WIN"
)

type Step struct {
	KifuId    string
	Timestamp int64
	Seq       int32

	Move     string
	Piece    Piece
	Finished Finished
	TimeSec  int64
	ThinkSec int64
	Note     []string
}

type Sfen struct {
	KifuId    string
	Timestamp int64

	Sfen string
}

type Position struct {
	KifuId    string
	Timestamp int64
	Seq       int32

	Position string
}

type GetOutput struct {
	Kifu        *Kifu
	OtherFields []*OtherField
	Players     []*Player
	Steps       []*Step
	Sfen        *Sfen
	Positions   []*Position
}

type Variant int

const (
	Variant_NULL Variant = iota
	Variant_PLAYERS
	Variant_STEPS
	Variant_SFEN
	Variant_POS
	Variant_OTHERS
)

type PutPlayer struct {
	Name string
	Note string
}

type PutStep struct {
	Seq      int32
	Move     string
	Piece    Piece
	Finished Finished
	TimeSec  int64
	ThinkSec int64
	Note     []string
}

type PostInput struct {
	UserId       string
	CreatedTs    int64
	StartTs      int64
	EndTs        int64
	Handicap     Handicap
	GameName     string
	OtherFields  map[string]string
	BlackPlayers []*PutPlayer
	WhitePlayers []*PutPlayer
	Sfen         string
	Steps        []*PutStep
}

type DB interface {
	Get(ctx context.Context, kifuId string, variants ...Variant) (*GetOutput, error)
	BatchGet(ctx context.Context, kifuIds []string, variants []Variant, f func(*GetOutput) bool) error
	Post(ctx context.Context, in *PostInput) (string, int64, error)
	DeleteKifu(ctx context.Context, kifuId string, version int64) error
}
