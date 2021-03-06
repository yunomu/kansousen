package db

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/golang/protobuf/proto"

	documentpb "github.com/yunomu/kansousen/proto/document"

	"github.com/yunomu/kansousen/lib/dynamodb"
	"github.com/yunomu/kansousen/lib/pbconv"
)

type DynamoDB struct {
	table       *dynamodb.DynamoDB
	parallelism int
}

var _ DB = (*DynamoDB)(nil)

type DynamoDBOption func(*DynamoDB)

func SetParallelism(i int) DynamoDBOption {
	return func(db *DynamoDB) {
		db.parallelism = i
	}
}

func NewDynamoDB(table *dynamodb.DynamoDB, ops ...DynamoDBOption) *DynamoDB {
	db := &DynamoDB{
		table:       table,
		parallelism: 2,
	}
	for _, f := range ops {
		f(db)
	}

	return db
}

func buildKifuKey(userId, kifuId string) (string, string) {
	return fmt.Sprintf("KIFU:%s", userId), kifuId
}

func buildStepKey(userId, kifuId string, seq int32) (string, string) {
	return fmt.Sprintf("STEP:%s:%s", userId, kifuId), fmt.Sprintf("%04d", seq)
}

func buildPositionKey(userId, sfenPos, kifuId string, seq int32) (string, string) {
	return fmt.Sprintf("POSITION:%s:%s", userId, sfenPos), fmt.Sprintf("%s:%04d", kifuId, seq)
}

func buildKifuSignatureKey(sfen, userId, kifuId string) (string, string) {
	return fmt.Sprintf("KIFU_SIG:%s", sfen), fmt.Sprintf("%s:%s", userId, kifuId)
}

func (db *DynamoDB) PutKifu(ctx context.Context,
	kifu *documentpb.Kifu,
	steps []*documentpb.Step,
) error {
	type writeItem struct {
		pk, sk  string
		doc     *documentpb.Document
		version int64
	}
	var wis []*writeItem

	pk, sk := buildKifuKey(kifu.UserId, kifu.KifuId)
	wis = append(wis, &writeItem{
		pk: pk,
		sk: sk,
		doc: &documentpb.Document{
			Select: &documentpb.Document_Kifu{
				Kifu: kifu,
			},
		},
		version: kifu.Version,
	})

	for _, step := range steps {
		pk, sk := buildStepKey(step.UserId, step.KifuId, step.Seq)
		wis = append(wis, &writeItem{
			pk: pk,
			sk: sk,
			doc: &documentpb.Document{
				Select: &documentpb.Document_Step{
					Step: step,
				},
			},
			version: step.Version,
		})
	}

	for _, p := range pbconv.StepsToPositions(steps) {
		pk, sk := buildPositionKey(p.UserId, p.Position, p.KifuId, p.Seq)
		wis = append(wis, &writeItem{
			pk: pk,
			sk: sk,
			doc: &documentpb.Document{
				Select: &documentpb.Document_Position{
					Position: p,
				},
			},
		})
	}

	pk, sk = buildKifuSignatureKey(kifu.Sfen, kifu.UserId, kifu.KifuId)
	wis = append(wis, &writeItem{
		pk: pk,
		sk: sk,
		doc: &documentpb.Document{
			Select: &documentpb.Document_KifuSignature{
				KifuSignature: &documentpb.KifuSignature{
					Sfen:      kifu.Sfen,
					UserId:    kifu.UserId,
					KifuId:    kifu.KifuId,
					CreatedTs: kifu.CreatedTs,
				},
			},
		},
	})

	var items []*dynamodb.WriteItem
	for _, writeItem := range wis {
		bytes, err := proto.Marshal(writeItem.doc)
		if err != nil {
			return err
		}

		items = append(items, &dynamodb.WriteItem{
			PK:      writeItem.pk,
			SK:      writeItem.sk,
			Bytes:   bytes,
			Version: writeItem.version,
		})
	}

	g, ctx := errgroup.WithContext(ctx)

	itemsCh := make(chan []*dynamodb.WriteItem, db.parallelism)
	g.Go(func() error {
		defer close(itemsCh)

		for i, l := 0, len(items); i < l; i += dynamodb.BatchWriteUnit {
			t := i + dynamodb.BatchWriteUnit
			if t > l {
				t = l
			}

			select {
			case itemsCh <- items[i:t]:
			case <-ctx.Done():
				if err := ctx.Err(); err == context.Canceled {
					return errors.New("canceled: split items")
				} else {
					return ctx.Err()
				}
			}
		}

		return nil
	})

	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for is := range itemsCh {
				if _, err := db.table.BatchPut(ctx, is); err != nil {
					return err
				}
			}

			return nil
		})
	}

	return g.Wait()
}

func (db *DynamoDB) GetKifu(
	ctx context.Context,
	userId string,
	kifuId string,
) (*documentpb.Kifu, error) {
	pk, sk := buildKifuKey(userId, kifuId)
	item, err := db.table.Get(ctx, pk, sk)
	if err != nil {
		return nil, err
	}

	doc := &documentpb.Document{}
	if err := proto.Unmarshal(item.Bytes, doc); err != nil {
		return nil, err
	}

	kifu := doc.GetKifu()
	if kifu == nil {
		return nil, ErrEmpty
	}
	kifu.Version = item.Version

	return kifu, nil
}

type StepSlice []*documentpb.Step

func (s StepSlice) Len() int               { return len(s) }
func (s StepSlice) Less(i int, j int) bool { return s[i].GetSeq() < s[j].GetSeq() }
func (s StepSlice) Swap(i int, j int)      { s[i], s[j] = s[j], s[i] }

func (db *DynamoDB) GetKifuAndSteps(
	ctx context.Context,
	userId string,
	kifuId string,
) (*documentpb.Kifu, []*documentpb.Step, error) {
	g, ctx := errgroup.WithContext(ctx)

	var kifu *documentpb.Kifu
	g.Go(func() error {
		k, err := db.GetKifu(ctx, userId, kifuId)
		if err != nil {
			return err
		}
		kifu = k

		return nil
	})

	var steps []*documentpb.Step
	g.Go(func() error {
		ss, err := db.GetSteps(ctx, userId, kifuId)
		if err != nil {
			return err
		}
		steps = ss

		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return kifu, steps, nil
}

func (db *DynamoDB) ListKifu(ctx context.Context, userId string, f func(kifu *documentpb.Kifu)) error {
	ctx, cancel := context.WithCancel(ctx)
	pk, _ := buildKifuKey(userId, "")
	var rerr error
	if err := db.table.Query(ctx, pk, func(item *dynamodb.Item) {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err == context.Canceled {
				rerr = errors.New("canceled: ListKifu(Scan)")
			} else {
				rerr = err
			}
			cancel()
			return
		default:
		}

		doc := &documentpb.Document{}
		if err := proto.Unmarshal(item.Bytes, doc); err != nil {
			rerr = err
			cancel()
			return
		}

		kifu := doc.GetKifu()
		if kifu == nil {
			rerr = ErrInvalidValue
			cancel()
			return
		}
		kifu.Version = item.Version

		f(kifu)
	}); err != nil {
		if rerr != nil {
			return rerr
		}
		return err
	}

	return nil
}

func (db *DynamoDB) DuplicateKifu(ctx context.Context, sfen string) ([]*documentpb.KifuSignature, error) {
	ctx, cancel := context.WithCancel(ctx)
	var rerr error
	var ret []*documentpb.KifuSignature
	pk, _ := buildKifuSignatureKey(sfen, "", "")
	if err := db.table.Query(ctx, pk, func(item *dynamodb.Item) {
		doc := &documentpb.Document{}
		if err := proto.Unmarshal(item.Bytes, doc); err != nil {
			rerr = err
			cancel()
			return
		}

		sig := doc.GetKifuSignature()
		if sig == nil {
			rerr = ErrInvalidValue
			cancel()
			return
		}
		sig.Version = item.Version

		ret = append(ret, sig)
	}); err != nil {
		if rerr != nil {
			return nil, rerr
		}
		return nil, err
	}

	return ret, nil
}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

func (db *DynamoDB) GetSteps(ctx context.Context, userId, kifuId string, options ...GetStepsOption) ([]*documentpb.Step, error) {
	o := &getStepsOptions{}
	for _, f := range options {
		f(o)
	}

	var opts []dynamodb.QueryOption
	switch {
	case o.start > 0 && o.end > 0:
		_, start := buildStepKey(userId, kifuId, o.start)
		_, end := buildStepKey(userId, kifuId, o.end)
		opts = append(opts, dynamodb.SetQueryRange(start, end))
	}

	pk, _ := buildStepKey(userId, kifuId, 0)
	ctx, cancel := context.WithCancel(ctx)
	var rerr error
	var ret []*documentpb.Step
	if err := db.table.Query(ctx, pk, func(item *dynamodb.Item) {
		doc := &documentpb.Document{}
		if err := proto.Unmarshal(item.Bytes, doc); err != nil {
			rerr = err
			cancel()
			return
		}

		step := doc.GetStep()
		if step == nil {
			rerr = ErrInvalidValue
			cancel()
			return
		}
		step.Version = item.Version

		ret = append(ret, step)
	}, opts...); err != nil {
		if rerr != nil {
			return nil, rerr
		}
		return nil, err
	}

	sort.Sort(StepSlice(ret))

	return ret, nil
}

func include(ss []string, o string) bool {
	for _, s := range ss {
		if s == o {
			return true
		}
	}
	return false
}

func (db *DynamoDB) getPositions(
	ctx context.Context,
	userId, pos string,
	opts *getSamePositionsOptions,
) ([]*PositionAndSteps, error) {
	g, ctx := errgroup.WithContext(ctx)

	posCh := make(chan *documentpb.Position)
	g.Go(func() error {
		defer close(posCh)

		pk, _ := buildPositionKey(userId, pos, "", 0)
		ctx, cancel := context.WithCancel(ctx)
		var rerr error
		if err := db.table.Query(ctx, pk, func(item *dynamodb.Item) {
			doc := &documentpb.Document{}
			if err := proto.Unmarshal(item.Bytes, doc); err != nil {
				rerr = err
				cancel()
				return
			}

			position := doc.GetPosition()
			if position == nil {
				rerr = ErrInvalidValue
				cancel()
				return
			}
			position.Version = item.Version

			if include(opts.excludeKifuIds, position.GetKifuId()) {
				return
			}

			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.Canceled {
					rerr = errors.New("canceled: send position")
				} else {
					rerr = err
				}
				cancel()
				return
			case posCh <- position:
			}
		}); err != nil {
			if rerr != nil {
				return rerr
			}
			return err
		}

		return nil
	})

	psCh := make(chan *PositionAndSteps)
	g.Go(func() error {
		for pos := range posCh {
			var steps []*documentpb.Step
			if opts.numStep > 0 {
				start := pos.Seq + 1
				end := start + opts.numStep - 1
				ss, err := db.GetSteps(ctx, pos.UserId, pos.KifuId, GetStepsSetRange(start, end))
				if err != nil {
					return err
				}
				steps = ss
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case psCh <- &PositionAndSteps{Position: pos, Steps: steps}:
			}
		}

		return nil
	})

	go func() {
		g.Wait()
		close(psCh)
	}()

	var ret []*PositionAndSteps
	for ps := range psCh {
		ret = append(ret, ps)
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ret, nil
}

func (db *DynamoDB) GetSamePositions(ctx context.Context, userIds []string, pos string, options ...GetSamePositionsOption) ([]*PositionAndSteps, error) {
	if pos == "" {
		return nil, ErrPositionIsEmpty
	}

	if len(userIds) == 0 {
		return nil, nil
	}

	o := &getSamePositionsOptions{}
	for _, f := range options {
		f(o)
	}

	g, ctx := errgroup.WithContext(ctx)

	userIdCh := make(chan string, db.parallelism)
	g.Go(func() error {
		defer close(userIdCh)

		for _, userId := range userIds {
			select {
			case userIdCh <- userId:
			case <-ctx.Done():
				if err := ctx.Err(); err == context.Canceled {
					return errors.New("canceled: paralellize user")
				} else {
					return err
				}
			}
		}

		return nil
	})

	psCh := make(chan []*PositionAndSteps)
	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for userId := range userIdCh {
				ps, err := db.getPositions(ctx, userId, pos, o)
				if err != nil {
					return err
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case psCh <- ps:
				}
			}

			return nil
		})
	}

	go func() {
		g.Wait()
		close(psCh)
	}()

	var ret []*PositionAndSteps
	for ps := range psCh {
		ret = append(ret, ps...)
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ret, nil
}

func (db *DynamoDB) GetRecentKifu(ctx context.Context, userId string, limit int) ([]*documentpb.Kifu, error) {
	var ret []*documentpb.Kifu

	pk, _ := buildKifuKey(userId, "")
	keys, err := db.table.RecentlyUpdated(ctx, pk, limit)
	if err != nil {
		return nil, err
	}

	items, err := db.table.BatchGet(ctx, keys)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		var doc documentpb.Document
		if err := proto.Unmarshal(item.Bytes, &doc); err != nil {
			return nil, err
		}

		kifu := doc.GetKifu()
		if kifu == nil {
			return nil, err
		}
		kifu.Version = item.Version

		ret = append(ret, kifu)
	}

	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (db *DynamoDB) getAllKifuKeys(ctx context.Context, userId, kifuId string) ([]*dynamodb.Key, error) {
	keyCh := make(chan *dynamodb.Key, db.parallelism)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		pk, sk := buildKifuKey(userId, kifuId)
		select {
		case keyCh <- &dynamodb.Key{PK: pk, SK: sk}:
		case <-ctx.Done():
			if err := ctx.Err(); err == context.Canceled {
				return errors.New("canceled: build kifu key")
			} else {
				return err
			}
		}

		return nil
	})

	g.Go(func() error {
		kifu, err := db.GetKifu(ctx, userId, kifuId)
		if err != nil {
			return err
		}

		pk, sk := buildKifuSignatureKey(kifu.Sfen, userId, kifuId)
		select {
		case keyCh <- &dynamodb.Key{PK: pk, SK: sk}:
		case <-ctx.Done():
			if err := ctx.Err(); err == context.Canceled {
				return errors.New("canceled: build signature key")
			} else {
				return err
			}
		}

		return nil
	})

	g.Go(func() error {
		steps, err := db.GetSteps(ctx, userId, kifuId)
		if err != nil {
			return err
		}

		for _, step := range steps {
			pk, sk := buildStepKey(step.UserId, step.KifuId, step.Seq)
			select {
			case keyCh <- &dynamodb.Key{PK: pk, SK: sk}:
			case <-ctx.Done():
				if err := ctx.Err(); err == context.Canceled {
					return errors.New("canceled: build step key")
				} else {
					return err
				}
			}
		}

		return nil
	})

	go func() {
		g.Wait()
		close(keyCh)
	}()

	var keys []*dynamodb.Key
	for key := range keyCh {
		keys = append(keys, key)
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return keys, nil
}

func (db *DynamoDB) DeleteKifu(ctx context.Context, userId, kifuId string) error {
	keys, err := db.getAllKifuKeys(ctx, userId, kifuId)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	keysCh := make(chan []*dynamodb.Key, db.parallelism)
	g.Go(func() error {
		defer close(keysCh)

		for i, l := 0, len(keys); i < l; i += dynamodb.BatchWriteUnit {
			t := i + dynamodb.BatchWriteUnit
			if t > l {
				t = l
			}

			select {
			case keysCh <- keys[i:t]:
			case <-ctx.Done():
				if err := ctx.Err(); err == context.Canceled {
					return errors.New("canceled: keys split")
				} else {
					return err
				}
			}
		}

		return nil
	})

	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for ks := range keysCh {
				if err := db.table.Delete(ctx, ks); err != nil {
					return err
				}
			}

			return nil
		})
	}

	return g.Wait()
}
