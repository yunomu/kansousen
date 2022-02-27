package db

import (
	"testing"

	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

func TestAttributeValue(t *testing.T) {
	av, err := dynamodbattribute.MarshalMap(DynamoDBKifuRecord{
		KifuId: "test-kifu-id",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if len(av) != 2 || av["kifuId"] == nil || av["var"] == nil {
		t.Fatalf("av: %#v", av)
	}
}

const (
	num  = 1000
	unit = 25
	para = 2

	sleep = 1 * time.Millisecond
)

func benchmarkSplitChan(ctx context.Context, as []int) ([]int, error) {
	g, ctx := errgroup.WithContext(ctx)

	asCh := make(chan []int)
	g.Go(func() error {
		defer close(asCh)

		var t []int
		for _, a := range as {
			t = append(t, a)
			if len(t) == unit {
				select {
				case asCh <- t:
				case <-ctx.Done():
					return ctx.Err()
				}
				t = nil
			}
		}

		select {
		case asCh <- t:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	var ret []int
	g.Go(func() error {
		for rs := range asCh {
			time.Sleep(sleep)
			ret = append(ret, rs...)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ret, nil
}

func benchmarkChanChan(ctx context.Context, as []int) ([]int, error) {
	g, ctx := errgroup.WithContext(ctx)

	aCh := make(chan int)
	g.Go(func() error {
		defer close(aCh)

		for _, a := range as {
			select {
			case aCh <- a:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	asCh := make(chan []int)
	g.Go(func() error {
		defer close(asCh)

		var t []int
		for a := range aCh {
			t = append(t, a)
			if len(t) == unit {
				select {
				case asCh <- t:
				case <-ctx.Done():
					return ctx.Err()
				}
				t = nil
			}
		}

		select {
		case asCh <- t:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	var ret []int
	g.Go(func() error {
		for rs := range asCh {
			time.Sleep(sleep)
			ret = append(ret, rs...)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ret, nil
}

func BenchmarkSplitChan(b *testing.B) {
	var as [num]int
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSplitChan(ctx, as[:])
	}
}

func BenchmarkChanChan(b *testing.B) {
	var as [num]int
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkChanChan(ctx, as[:])
	}
}
