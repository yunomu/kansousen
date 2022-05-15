package handler

import (
	"context"
	"errors"

	"github.com/yunomu/kansousen/graphql/model"
	"github.com/yunomu/kansousen/lib/kifudb"
)

type Handler struct {
	db kifudb.DB
}

func NewHandler(db kifudb.DB) *Handler {
	return &Handler{
		db: db,
	}
}

type Request struct {
	Id string `json:"id"`
}

type Response struct {
	KifuId string `json:"kifu_id"`
}

var pathToVariantMap = map[string]kifudb.Variant{
	"createdTs":         kifudb.Var_CreatedTs,
	"startTs":           kifudb.Var_StartTs,
	"endTs":             kifudb.Var_EndTs,
	"handicap":          kifudb.Var_Handicap,
	"note":              kifudb.Var_Note,
	"otherFields/name":  kifudb.Var_OtherName,
	"otherFields/value": kifudb.Var_OtherValue,
	"players/name":      kifudb.Var_PlayerName,
	"players/order":     kifudb.Var_PlayerOrder,
	"players/note":      kifudb.Var_PlayerNote,
	"steps/move":        kifudb.Var_Move,
	"steps/piece":       kifudb.Var_Piece,
	"steps/position":    kifudb.Var_Position,
	"steps/finished":    kifudb.Var_Finished,
	"steps/timeSec":     kifudb.Var_TimeSec,
	"steps/thinkSec":    kifudb.Var_ThinkSec,
	"steps/notes/id":    kifudb.Var_StepNoteId,
	"steps/notes/text":  kifudb.Var_StepNoteText,
	"sfen":              kifudb.Var_Sfen,
}

func selectionsToVariants(selectionSet []string) []kifudb.Variant {
	vars := []kifudb.Variant{
		kifudb.Var_KifuId,
		kifudb.Var_Type,
		kifudb.Var_Seq,
		kifudb.Var_UserId,
		kifudb.Var_Timestamp,
	}

	for _, sl := range selectionSet {
		v, ok := pathToVariantMap[sl]
		if !ok {
			continue
		}

		vars = append(vars, v)
	}

	return vars
}

func (h *Handler) Serve(ctx context.Context, userId string, selectionSet []string, req *Request) (*model.Kifu, error) {
	vars := selectionsToVariants(selectionSet)

	recs, err := h.db.GetKifu(ctx, req.Id, vars)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, errors.New("Not found")
	}

	var kifu model.Kifu
	var steps []*model.Step
	posMap := make(map[int]string)
	stepNoteMap := make(map[int][]*model.StepNote)
	for _, rec_ := range recs {
		if rec_.UserId != userId {
			continue
		}

		rec := rec_
		switch kifudb.Type(rec.Type) {
		case kifudb.Type_Kifu:
			kifu.KifuID = rec.KifuId
			kifu.UserID = &rec.UserId
			if t := rec.Timestamp; t != 0 {
				fl := float64(t)
				kifu.Timestamp = &fl
			}
			if t := rec.CreatedTs; t != 0 {
				fl := float64(t)
				kifu.CreatedTs = &fl
			}
			if t := rec.StartTs; t != 0 {
				fl := float64(t)
				kifu.StartTs = &fl
			}
			if t := rec.EndTs; t != 0 {
				fl := float64(t)
				kifu.EndTs = &fl
			}
			if h := model.Handicap(rec.Handicap); h.IsValid() {
				kifu.Handicap = &h
			}
			if s := rec.Note; len(s) != 0 {
				kifu.Note = &s
			}

		case kifudb.Type_Player:
			var player model.Player
			player.Name = rec.PlayerName
			if o := model.Order(rec.PlayerOrder); o.IsValid() {
				player.Order = o
			}
			if t := rec.PlayerNote; len(t) != 0 {
				player.Note = &t
			}
			kifu.Players = append(kifu.Players, &player)

		case kifudb.Type_Step:
			var step model.Step

			step.Seq = int(rec.Seq)
			if t := rec.Move; len(t) != 0 {
				step.Move = &t
			}
			if p := model.Piece(rec.Piece); p.IsValid() {
				step.Piece = &p
			}
			if s := model.FinishedStatus(rec.Finished); s.IsValid() {
				step.Finished = &s
			}
			if t := rec.TimeSec; t != 0 {
				i := int(t)
				step.TimeSec = &i
			}
			if t := rec.ThinkSec; t != 0 {
				i := int(t)
				step.ThinkSec = &i
			}

		case kifudb.Type_SFEN:
			if t := rec.Sfen; len(t) != 0 {
				kifu.Sfen = &t
			}

		case kifudb.Type_Pos:
			if t := rec.Position; len(t) != 0 {
				posMap[int(rec.Seq)] = t
			}

		case kifudb.Type_Other:
			var other model.OtherField
			other.Name = rec.OtherName
			if t := rec.OtherValue; len(t) != 0 {
				other.Value = &t
			}
			kifu.OtherFields = append(kifu.OtherFields, &other)

		case kifudb.Type_StepNote:
			var stepNote model.StepNote
			stepNote.ID = int(rec.StepNoteId)
			if t := rec.StepNoteText; len(t) != 0 {
				stepNote.Text = &t
			}
			seq := int(rec.Seq)
			stepNoteMap[seq] = append(stepNoteMap[seq], &stepNote)
		}
	}

	if len(steps) != 0 || !(len(posMap) == 0 && len(stepNoteMap) == 0) {
		for i, step := range steps {
			if pos, ok := posMap[step.Seq]; ok {
				steps[i].Position = &pos
			}
			if stepNote, ok := stepNoteMap[step.Seq]; ok {
				steps[i].Notes = stepNote
			}
		}
	}
	kifu.Steps = steps

	return &kifu, nil
}
