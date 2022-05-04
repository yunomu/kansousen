package db

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

func (d *DynamoDB) GetKifu(ctx context.Context, kifuId string, types []Type, vars []Variant) ([]*Record, error) {
	if len(types) == 0 || len(vars) == 0 {
		return nil, nil
	}

	var expressions []string
	attrNames := make(map[string]*string)
	attrValues := make(map[string]*dynamodb.AttributeValue)

	expressions = append(expressions, "#kifuId = :kifuId")
	attrNames["#kifuId"] = aws.String("kifuId")
	attrValues[":kifuId"] = &dynamodb.AttributeValue{S: aws.String(kifuId)}

	attrNames["#type"] = aws.String("type")
	var typeOps []string
	for i, t_ := range types {
		if !t_.isValid() {
			continue
		}
		t := string(t_)

		op := fmt.Sprintf(":type%d", i)
		attrValues[op] = &dynamodb.AttributeValue{S: aws.String(t)}
		typeOps = append(typeOps, op)
	}
	expressions = append(expressions, fmt.Sprintf("#type IN (%s)", strings.Join(typeOps, ",")))

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

	// TODO
	var _ = records

	return nil, nil
}
