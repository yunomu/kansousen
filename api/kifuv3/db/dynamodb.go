package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"google.golang.org/protobuf/proto"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

	documentpb "github.com/yunomu/kansousen/proto/document"
)

const (
	kifuAttr      = "kifu"
	stepAttr      = "step"
	versionAttr   = "version"
	seqAttr       = "seq"
	userIdAttr    = "userId"
	kifuIdAttr    = "kifuId"
	createdTsAttr = "createdTs"
	sfenAttr      = "sfen"
	posAttr       = "pos"

	kifuVar       = "KIFU"
	stepVarPrefix = "STEP:"

	BatchUnit = 25
)

func stepVar(seq int32) string {
	return fmt.Sprintf("%s%d", stepVarPrefix, seq)
}

func isStepVar(s string) bool {
	return strings.HasPrefix(s, stepVarPrefix)
}

type DynamoDBKifuRecord struct {
	KifuId      string   `dynamodbav:"KifuId"`
	Var         string   `dynamodbav:"Var"`
	UserId      string   `dynamodbav:"UserId,omitempty"`
	Timestamp   int64    `dynamodbav:"Timestamp,omitempty"`
	CreatedTs   int64    `dynamodbav:"CreatedTs,omitempty"`
	StartTs     int64    `dynamodbav:"StartTs,omitempty"`
	EndTs       int64    `dynamodbav:"EndTs,omitempty"`
	Handicap    string   `dynamodbav:"Handicap,omitempty"`
	GameName    string   `dynamodbav:"GameName,omitempty"`
	OtherName   string   `dynamodbav:"OtherName,omitempty"`
	OtherValue  string   `dynamodbav:"OtherValue,omitempty"`
	PlayerName  string   `dynamodbav:"PlayerName,omitempty"`
	PlayerOrder string   `dynamodbav:"PlayerOrder,omitempty"`
	PlayerNote  string   `dynamodbav:"PlayerNote,omitempty"`
	Sfen        string   `dynamodbav:"Sfen,omitempty"`
	Seq         int32    `dynamodbav:"Seq,omitempty"`
	Move        string   `dynamodbav:"Move,omitempty"`
	Piece       string   `dynamodbav:"Piece,omitempty"`
	Position    string   `dynamodbav:"Position,omitempty"`
	Finished    string   `dynamodbav:"Finished,omitempty"`
	TimeSec     int32    `dynamodbav:"TimeSec,omitempty"`
	ThinkSec    int32    `dynamodbav:"ThinkSec,omitempty"`
	StepNote    []string `dynamodbav:"StepNote,omitempty"`
}

type DynamoDB struct {
	client    *dynamodb.DynamoDB
	tableName string

	parallelism int
}

var _ DB = (*DynamoDB)(nil)

type DynamoDBOption func(*DynamoDB)

func SetParallelism(i int) DynamoDBOption {
	return func(db *DynamoDB) {
		db.parallelism = i
	}
}

func NewDynamoDB(client *dynamodb.DynamoDB, tableName string, ops ...DynamoDBOption) *DynamoDB {
	db := &DynamoDB{
		client:    client,
		tableName: tableName,

		parallelism: 2,
	}
	for _, f := range ops {
		f(db)
	}

	return db
}

func (db *DynamoDB) batchWrite(
	ctx context.Context,
	g *errgroup.Group,
	reqCh chan *dynamodb.WriteRequest,
) error {
	reqsCh := make(chan []*dynamodb.WriteRequest, db.parallelism)
	g.Go(func() error {
		defer close(reqsCh)

		var reqs []*dynamodb.WriteRequest
		for req := range reqCh {
			reqs = append(reqs, req)
			if len(reqs) == BatchUnit {
				select {
				case reqsCh <- reqs:
				case <-ctx.Done():
					return ctx.Err()
				}
				reqs = nil
			}
		}

		select {
		case reqsCh <- reqs:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for reqs := range reqsCh {
				out, err := db.client.BatchWriteItemWithContext(ctx, &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]*dynamodb.WriteRequest{
						db.tableName: reqs,
					},
				})
				if err == nil {
					return err
				}

				var _ = out
			}

			return nil
		})
	}

	return g.Wait()
}

func (db *DynamoDB) PutKifu(
	ctx context.Context,
	kifu *documentpb.Kifu,
	steps []*documentpb.Step,
	version int64,
) (int64, error) {
	stepNum := int32(len(steps))
	bs, err := proto.Marshal(kifu)
	if err != nil {
		return 0, err
	}
	newVersion := time.Now().UnixNano()
	kifuAv, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
		UserId:    kifu.GetUserId(),
		KifuId:    kifu.GetKifuId(),
		Var:       kifuVar,
		CreatedTs: kifu.GetCreatedTs(),
		StartTs:   kifu.GetStartTs(),
		Sfen:      kifu.GetSfen(),
		Kifu:      bs,
		Version:   newVersion,
		StepNum:   stepNum,
	})
	if err != nil {
		return 0, err
	}

	out, err := db.client.PutItemWithContext(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(db.tableName),

		ConditionExpression: aws.String("attribute_not_exists(#version) OR #version = :version"),
		ExpressionAttributeNames: map[string]*string{
			"#version": aws.String(versionAttr),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":version": &dynamodb.AttributeValue{
				N: aws.String(fmt.Sprintf("%d", version)),
			},
		},
		Item: kifuAv,

		ReturnValues: aws.String("ALL_OLD"),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case dynamodb.ErrCodeConditionalCheckFailedException:
				return 0, ErrLockError
			}
		}
		return 0, err
	}

	var old DynamoDBKifuRecord
	if len(out.Attributes) != 0 {
		if err := dynamodbattribute.UnmarshalMap(out.Attributes, &old); err != nil {
			return 0, err
		}
	}

	g, ctx := errgroup.WithContext(ctx)

	reqCh := make(chan *dynamodb.WriteRequest)
	g.Go(func() error {
		defer close(reqCh)

		for _, step := range steps {
			bs, err := proto.Marshal(kifu)
			if err != nil {
				return err
			}
			av, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
				UserId: step.GetUserId(),
				KifuId: step.GetKifuId(),
				Var:    stepVar(step.GetSeq()),
				Seq:    step.GetSeq(),
				Pos:    step.GetPosition(),
				Step:   bs,
			})

			select {
			case reqCh <- &dynamodb.WriteRequest{
				PutRequest: &dynamodb.PutRequest{Item: av},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		for i := stepNum + 1; i <= old.StepNum; i++ {
			av, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
				KifuId: old.KifuId,
				Var:    stepVar(i),
			})
			if err != nil {
				return err
			}

			select {
			case reqCh <- &dynamodb.WriteRequest{
				DeleteRequest: &dynamodb.DeleteRequest{Key: av},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	if err := db.batchWrite(ctx, g, reqCh); err != nil {
		return 0, err
	}

	return newVersion, err
}

type StepSlice []*documentpb.Step

func (s StepSlice) Len() int               { return len(s) }
func (s StepSlice) Less(i int, j int) bool { return s[i].GetSeq() < s[j].GetSeq() }
func (s StepSlice) Swap(i int, j int)      { s[i], s[j] = s[j], s[i] }

func (db *DynamoDB) GetKifu(
	ctx context.Context,
	kifuId string,
	offset int,
	length int,
) (*GetItem, error) {
	g, ctx := errgroup.WithContext(ctx)

	itemsCh := make(chan []map[string]*dynamodb.AttributeValue, db.parallelism)
	g.Go(func() error {
		defer close(itemsCh)

		var rerr error
		if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(db.tableName),
			KeyConditionExpression: aws.String("#kifuId = :kifuId"),
			ExpressionAttributeNames: map[string]*string{
				"#kifuId": aws.String(kifuIdAttr),
			},
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":kifuId": &dynamodb.AttributeValue{S: aws.String(kifuId)},
			},
			ProjectionExpression: aws.String(strings.Join([]string{kifuAttr, versionAttr, stepAttr, seqAttr}, ",")),
		}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
			select {
			case <-ctx.Done():
				rerr = ctx.Err()
				return false
			case itemsCh <- out.Items:
				return true
			}
		}); err != nil {
			return err
		}

		return rerr
	})

	var kifu *documentpb.Kifu
	var version int64
	stepsCh := make(chan []*documentpb.Step, db.parallelism)
	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for items := range itemsCh {
				recs := []DynamoDBKifuRecord{}
				if err := dynamodbattribute.UnmarshalListOfMaps(items, &recs); err != nil {
					return err
				}

				var steps []*documentpb.Step
				for _, rec := range recs {
					switch {
					case rec.Var == kifuVar: // Kifu
						var k documentpb.Kifu
						if err := proto.Unmarshal(rec.Kifu, &k); err != nil {
							return &ErrInvalidValue{
								Details: err.Error(),
							}
						}

						kifu = &k
						version = rec.Version
					case isStepVar(rec.Var): // Step
						var s documentpb.Step
						if err := proto.Unmarshal(rec.Step, &s); err != nil {
							return &ErrInvalidValue{
								Details: err.Error(),
							}
						}

						steps = append(steps, &s)
					default:
						return fmt.Errorf("")
					}
				}

				select {
				case stepsCh <- steps:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		})
	}

	go func() {
		g.Wait()
		close(stepsCh)
	}()

	var steps []*documentpb.Step
	for ss := range stepsCh {
		steps = append(steps, ss...)
	}

	if err := g.Wait(); err != nil {
		return nil, nil, 0, err
	}

	sort.Sort(StepSlice(steps))

	return kifu, steps, version, nil
}

type versionedKifu struct {
	kifu    *documentpb.Kifu
	version int64
}

func (db *DynamoDB) ListKifu(ctx context.Context, userId string, f func(kifu *documentpb.Kifu, version int64)) error {
	g, ctx := errgroup.WithContext(ctx)

	itemsCh := make(chan []map[string]*dynamodb.AttributeValue, db.parallelism)
	g.Go(func() error {
		defer close(itemsCh)

		var rerr error
		if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(db.tableName),
			IndexName:              aws.String("User"),
			KeyConditionExpression: aws.String("#userId = :userId AND #seq = :seq"),
			ExpressionAttributeNames: map[string]*string{
				"#userId": aws.String(userIdAttr),
				"#seq":    aws.String(seqAttr),
			},
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":userId": &dynamodb.AttributeValue{S: aws.String(userId)},
				":seq":    &dynamodb.AttributeValue{N: aws.String("0")},
			},
			ProjectionExpression: aws.String(strings.Join([]string{kifuAttr, versionAttr}, ",")),
		}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
			select {
			case <-ctx.Done():
				rerr = ctx.Err()
				return false
			case itemsCh <- out.Items:
				return true
			}
		}); err != nil {
			return err
		}

		return rerr
	})

	ch := make(chan *versionedKifu, db.parallelism)
	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for items := range itemsCh {
				recs := []DynamoDBKifuRecord{}
				if err := dynamodbattribute.UnmarshalListOfMaps(items, &recs); err != nil {
					return err
				}

				for _, rec := range recs {
					var kifu documentpb.Kifu
					if err := proto.Unmarshal(rec.Kifu, &kifu); err != nil {
						return &ErrInvalidValue{
							Details: err.Error(),
						}
					}

					select {
					case ch <- &versionedKifu{kifu: &kifu, version: rec.Version}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			return nil
		})
	}

	go func() {
		g.Wait()
		close(ch)
	}()

	for vk := range ch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			f(vk.kifu, vk.version)
		}
	}

	return g.Wait()
}

func (db *DynamoDB) GetKifuIdsBySfen(ctx context.Context, sfen string) ([]*UserKifu, error) {
	var ret []*UserKifu
	var rerr error
	if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(db.tableName),
		IndexName:              aws.String("Sfen"),
		KeyConditionExpression: aws.String("#sfen = :sfen"),
		ExpressionAttributeNames: map[string]*string{
			"#sfen": aws.String(sfenAttr),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":sfen": &dynamodb.AttributeValue{S: aws.String(sfen)},
		},
		ProjectionExpression: aws.String(strings.Join([]string{kifuIdAttr, userIdAttr}, ",")),
	}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
		select {
		case <-ctx.Done():
			rerr = ctx.Err()
			return false
		default:
		}

		var records []DynamoDBKifuRecord
		if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &records); err != nil {
			rerr = err
			return false
		}

		for _, r := range records {
			ret = append(ret, &UserKifu{
				UserId: r.UserId,
				KifuId: r.KifuId,
			})
		}

		return true
	}); err != nil {
		return nil, err
	} else if rerr != nil {
		return nil, rerr
	}

	return ret, nil
}

type stepKey struct {
	kifuId string
	userId string
	seq    int32
}

func (db *DynamoDB) GetSamePositions(ctx context.Context, userIds []string, pos string, options ...GetSamePositionsOption) ([]*Position, error) {
	opts := &getSamePositionsOptions{
		numStep: 5,
	}
	for _, f := range options {
		f(opts)
	}

	g, ctx := errgroup.WithContext(ctx)

	stepKeyCh := make(chan *stepKey, db.parallelism)
	g.Go(func() error {
		defer close(stepKeyCh)

		var rerr error
		if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(db.tableName),
			IndexName:              aws.String("Position"),
			KeyConditionExpression: aws.String("#pos = :pos"),
			ExpressionAttributeNames: map[string]*string{
				"#pos": aws.String(posAttr),
			},
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":pos": &dynamodb.AttributeValue{S: aws.String(pos)},
			},
			ProjectionExpression: aws.String(strings.Join([]string{kifuIdAttr, seqAttr, userIdAttr}, ",")),
		}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
			select {
			case <-ctx.Done():
				rerr = ctx.Err()
				return false
			default:
			}

			var records []DynamoDBKifuRecord
			if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &records); err != nil {
				rerr = err
				return false
			}

			for _, r := range records {
				select {
				case stepKeyCh <- &stepKey{
					kifuId: r.KifuId,
					userId: r.UserId,
					seq:    r.Seq,
				}:
				case <-ctx.Done():
					rerr = ctx.Err()
					return false
				}
			}

			return true
		}); err != nil {
			return err
		}

		return rerr
	})

	posCh := make(chan *Position, db.parallelism)
	for i := 0; i < db.parallelism; i++ {
		g.Go(func() error {
			for stepKey := range stepKeyCh {
				var steps []*documentpb.Step
				var rerr error
				if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
					TableName:              aws.String(db.tableName),
					KeyConditionExpression: aws.String("#kifuId = :kifuId AND #seq >= :seqStart AND #seq < :seqEnd"),
					ExpressionAttributeNames: map[string]*string{
						"#kifuId": aws.String(kifuIdAttr),
						"#seq":    aws.String(seqAttr),
					},
					ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
						":kifuId":   &dynamodb.AttributeValue{S: aws.String(stepKey.kifuId)},
						":seqStart": &dynamodb.AttributeValue{N: aws.String(fmt.Sprintf("%d", stepKey.seq))},
						":seqEnd":   &dynamodb.AttributeValue{N: aws.String(fmt.Sprintf("%d", int32(stepKey.seq)+opts.numStep))},
					},
					ProjectionExpression: aws.String(stepAttr),
				}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
					select {
					case <-ctx.Done():
						rerr = ctx.Err()
						return false
					default:
					}

					var records []DynamoDBKifuRecord
					if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &records); err != nil {
						rerr = err
						return false
					}
					for _, r := range records {
						var step documentpb.Step
						if err := proto.Unmarshal(r.Step, &step); err != nil {
							rerr = err
							return false
						}

						steps = append(steps, &step)
					}

					return true
				}); err != nil {
					return err
				} else if rerr != nil {
					return rerr
				}

				select {
				case posCh <- &Position{
					KifuId: stepKey.kifuId,
					UserId: stepKey.userId,
					Steps:  steps,
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		})
	}

	go func() {
		g.Wait()
		close(posCh)
	}()

	var ret []*Position
	for pos := range posCh {
		ret = append(ret, pos)
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ret, nil
}

func (db *DynamoDB) GetRecentKifu(ctx context.Context, userId string, limit int) ([]*documentpb.Kifu, error) {
	var ret []*documentpb.Kifu
	var rerr error
	if err := db.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(db.tableName),
		IndexName:              aws.String("Created"),
		KeyConditionExpression: aws.String("#userId = :userId"),
		ExpressionAttributeNames: map[string]*string{
			"#userId": aws.String(userIdAttr),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":userId": &dynamodb.AttributeValue{S: aws.String(userId)},
		},
		ProjectionExpression: aws.String(kifuAttr),
		ScanIndexForward:     aws.Bool(false),
		Limit:                aws.Int64(int64(limit)),
	}, func(out *dynamodb.QueryOutput, lastPage bool) bool {
		select {
		case <-ctx.Done():
			rerr = ctx.Err()
			return false
		default:
		}

		var records []DynamoDBKifuRecord
		if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &records); err != nil {
			rerr = err
			return false
		}
		for _, rec := range records {
			var kifu documentpb.Kifu
			if err := proto.Unmarshal(rec.Kifu, &kifu); err != nil {
				rerr = err
				return false
			}

			ret = append(ret, &kifu)
		}

		return true
	}); err != nil {
		return nil, err
	} else if rerr != nil {
		return nil, rerr
	}

	return ret, nil
}

func (db *DynamoDB) DeleteKifu(ctx context.Context, kifuId string, version int64) error {
	key, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
		KifuId: kifuId,
		Var:    kifuVar,
	})
	if err != nil {
		return err
	}

	out, err := db.client.DeleteItemWithContext(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(db.tableName),

		ConditionExpression: aws.String("#version == :version"),
		ExpressionAttributeNames: map[string]*string{
			"#version": aws.String(versionAttr),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":version": &dynamodb.AttributeValue{
				N: aws.String(fmt.Sprintf("%d", version)),
			},
		},
		Key: key,

		ReturnValues: aws.String("ALL_OLD"),
	})
	if err != nil {
		return err
	}

	var old DynamoDBKifuRecord
	if err := dynamodbattribute.UnmarshalMap(out.Attributes, &old); err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	reqCh := make(chan *dynamodb.WriteRequest)
	g.Go(func() error {
		defer close(reqCh)

		for i := int32(1); i <= old.StepNum; i++ {
			key, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
				KifuId: kifuId,
				Var:    kifuVar,
			})
			if err != nil {
				return err
			}

			select {
			case reqCh <- &dynamodb.WriteRequest{
				DeleteRequest: &dynamodb.DeleteRequest{Key: key},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}

		}

		return nil
	})

	return db.batchWrite(ctx, g, reqCh)
}
