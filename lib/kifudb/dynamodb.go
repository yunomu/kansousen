package kifudb

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

type DynamoDB struct {
	client *dynamodb.DynamoDB
	table  string

	createdIndex string
}

var _ DB = (*DynamoDB)(nil)

func NewDynamoDB(
	client *dynamodb.DynamoDB,
	table string,
) *DynamoDB {
	return &DynamoDB{
		client: client,
		table:  table,
	}
}

func variantsToTypes(vars []Variant) []Type {
	types := make(map[Type]struct{})

	for _, v := range vars {
		switch v {
		case Var_CreatedTs, Var_StartTs, Var_EndTs, Var_Handicap, Var_GameName, Var_Note:
			types[Type_Kifu] = struct{}{}
		case Var_PlayerName, Var_PlayerOrder, Var_PlayerNote:
			types[Type_Player] = struct{}{}
		case Var_Move, Var_Piece, Var_Finished, Var_TimeSec, Var_ThinkSec:
			types[Type_Step] = struct{}{}
		case Var_Sfen:
			types[Type_SFEN] = struct{}{}
		case Var_Position:
			types[Type_Pos] = struct{}{}
		case Var_OtherName, Var_OtherValue:
			types[Type_Other] = struct{}{}
		case Var_StepNoteId, Var_StepNoteText:
			types[Type_StepNote] = struct{}{}
		}
	}

	var ret []Type
	for t, _ := range types {
		ret = append(ret, t)
	}

	if len(ret) == 0 {
		ret = append(ret, Type_Kifu)
	}

	return ret
}

func (d *DynamoDB) GetKifu(ctx context.Context, kifuId string, vars []Variant) ([]*Record, error) {
	if len(vars) == 0 {
		return nil, nil
	}

	types := variantsToTypes(vars)

	var expressions []string
	attrNames := make(map[string]*string)
	attrValues := make(map[string]*dynamodb.AttributeValue)

	expressions = append(expressions, "#kifuId = :kifuId")
	attrNames["#kifuId"] = aws.String("kifuId")
	attrValues[":kifuId"] = &dynamodb.AttributeValue{S: aws.String(kifuId)}

	attrNames["#sk"] = aws.String("sk")
	var typeOps []string
	for i, t_ := range types {
		if !t_.isValid() {
			continue
		}
		t := string(t_)

		typeOps = append(typeOps, fmt.Sprintf("begins_with(#sk,:type%d)", i))
		attrValues[fmt.Sprintf(":type%d", i)] = &dynamodb.AttributeValue{
			S: aws.String(fmt.Sprintf("%s:", t)),
		}
	}
	expressions = append(expressions, "("+strings.Join(typeOps, " OR ")+")")

	var projections []string
	for _, v_ := range vars {
		if !v_.isValid() {
			continue
		}
		v := string(v_)

		n := "#" + v
		attrNames[n] = aws.String(v)
		projections = append(projections, n)
	}

	var rerr error
	var records []*Record
	if err := d.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
		TableName: aws.String(d.table),

		KeyConditionExpression:    aws.String(strings.Join(expressions, " AND ")),
		ProjectionExpression:      aws.String(strings.Join(projections, ",")),
		ExpressionAttributeNames:  attrNames,
		ExpressionAttributeValues: attrValues,
	}, func(out *dynamodb.QueryOutput, last bool) bool {
		recs := []Record{}
		if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &recs); err != nil {
			rerr = err
			return false
		}

		for _, r_ := range recs {
			r := r_
			records = append(records, &r)
		}

		return true
	}); err != nil {
		return nil, err
	} else if rerr != nil {
		return nil, rerr
	}

	return records, nil
}

func (d *DynamoDB) RecentKifus(ctx context.Context, userId string, limit int, variants []Variant) ([]*Record, error) {
	var ret []*Record

	var rerr error
	if err := d.client.QueryPagesWithContext(ctx, &dynamodb.QueryInput{
		TableName: aws.String(d.table),
		IndexName: aws.String(d.createdIndex),

		KeyConditionExpression: aws.String("#userId = :userId"),
		ProjectionExpression:   aws.String("#kifuId"),
		ExpressionAttributeNames: map[string]*string{
			"#userId": aws.String("userId"),
			"#kifuId": aws.String("kifuId"),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":userId": {S: aws.String(userId)},
		},

		ScanIndexForward: aws.Bool(false),
	}, func(out *dynamodb.QueryOutput, last bool) bool {
		recs := []Record{}
		if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &recs); err != nil {
			rerr = err
			return false
		}

		for _, r_ := range recs {
			r := r_
			ret = append(ret, &r)
		}

		return true
	}); err != nil {
		return nil, err
	} else if rerr != nil {
		return nil, rerr
	}

	return ret, nil
}
