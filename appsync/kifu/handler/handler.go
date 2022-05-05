package handler

import (
	"context"
	"strings"

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

func selectionsToVariants(selectionSet []string) ([]kifudb.Variant, []kifudb.Type) {
	var vars []kifudb.Variant
	vars = append(vars, kifudb.Var_KifuId, kifudb.Var_Type, kifudb.Var_Seq)

	var types []kifudb.Type
	types = append(types, kifudb.Type_Kifu)

	for _, sl := range selectionSet {
		ss := strings.Split(sl, "/")
		l := len(ss)
		if l == 0 {
			continue
		}

		switch ss[0] {
		case "kifuId":
			// default value
		case "userId":
			vars = append(vars, kifudb.Var_UserId)
		case "timestamp":
			vars = append(vars, kifudb.Var_Timestamp)
		case "createdTs":
			vars = append(vars, kifudb.Var_CreatedTs)
		case "startTs":
			vars = append(vars, kifudb.Var_StartTs)
		case "endTs":
			vars = append(vars, kifudb.Var_EndTs)
		case "handicap":
			vars = append(vars, kifudb.Var_Handicap)
		case "note":
			if l == 1 {
				vars = append(vars, kifudb.Var_Note)
				break
			}
		case "otherFields":
			switch l {
			case 1:
				types = append(types, kifudb.Type_Other)
				break
			case 2:
				switch ss[1] {
				case "name":
					vars = append(vars, kifudb.Var_OtherName)
				case "value":
					vars = append(vars, kifudb.Var_OtherValue)
				}
			}
		case "players":
			switch l {
			case 1:
				types = append(types, kifudb.Type_Player)
			case 2:
				switch ss[1] {
				case "name":
					vars = append(vars, kifudb.Var_PlayerName)
				case "order":
					vars = append(vars, kifudb.Var_PlayerOrder)
				case "note":
					vars = append(vars, kifudb.Var_PlayerNote)
				}
			}
		case "steps":
			if l == 1 {
				types = append(types, kifudb.Type_Step)
				break
			}

			switch ss[1] {
			case "seq":
				// default value
			case "move":
				vars = append(vars, kifudb.Var_Move)
			case "piece":
				vars = append(vars, kifudb.Var_Piece)
			case "position":
				types = append(types, kifudb.Type_Pos)
				vars = append(vars, kifudb.Var_Position)
			case "finished":
				vars = append(vars, kifudb.Var_Finished)
			case "timeSec":
				vars = append(vars, kifudb.Var_TimeSec)
			case "thinkSec":
				vars = append(vars, kifudb.Var_ThinkSec)
			case "notes":
				if l == 2 {
					types = append(types, kifudb.Type_StepNote)
					break
				}
				switch ss[2] {
				case "id":
					vars = append(vars, kifudb.Var_StepNoteId)
				case "text":
					vars = append(vars, kifudb.Var_StepNoteText)
				}
			}
		case "sfen":
			if l != 1 {
				break
			}
			types = append(types, kifudb.Type_SFEN)
			vars = append(vars, kifudb.Var_Sfen)
		}
	}

	return vars, types
}

func (h *Handler) Serve(ctx context.Context, selectionSet []string, req *Request) (*model.Kifu, error) {
	vars, types := selectionsToVariants(selectionSet)

	recs, err := h.db.GetKifu(ctx, req.Id, types, vars)
	if err != nil {
		return nil, err
	}

	var kifu model.Kifu
	var steps []*model.Step
	posMap := make(map[int]string)
	stepNoteMap := make(map[int][]*model.StepNote)
	for _, rec_ := range recs {
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
