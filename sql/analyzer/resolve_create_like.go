package analyzer

import (
	"fmt"
	"strings"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/plan"
)

func resolveCreateLike(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, error) {
	planCreate, ok := n.(*plan.CreateTable)
	if !ok || planCreate.Like() == nil {
		return n, nil
	}
	resolvedLikeTable, ok := planCreate.Like().(*plan.ResolvedTable)
	if !ok {
		return nil, fmt.Errorf("attempted to resolve CREATE LIKE, expected `ResolvedTable` but received `%T`", planCreate.Like())
	}
	likeTable := resolvedLikeTable.Table
	var idxDefs []*plan.IndexDefinition
	if indexableTable, ok := likeTable.(sql.IndexedTable); ok {
		indexes, err := indexableTable.GetIndexes(ctx)
		if err != nil {
			return nil, err
		}
		for _, index := range indexes {
			constraint := sql.IndexConstraint_None
			if index.IsUnique() {
				constraint = sql.IndexConstraint_Unique
			}
			columns := make([]sql.IndexColumn, len(index.Expressions()))
			for i, col := range index.Expressions() {
				//TODO: find a better way to get only the column name if the table is present
				col = strings.TrimPrefix(col, indexableTable.Name()+".")
				columns[i] = sql.IndexColumn{
					Name:   col,
					Length: 0,
				}
			}
			idxDefs = append(idxDefs, &plan.IndexDefinition{
				IndexName:  index.ID(),
				Using:      sql.IndexUsing_Default,
				Constraint: constraint,
				Columns:    columns,
				Comment:    index.Comment(),
			})
		}
	}
	origSch := likeTable.Schema()
	newSch := make(sql.Schema, len(origSch))
	for i, col := range origSch {
		tempCol := *col
		tempCol.Source = planCreate.Name()
		newSch[i] = &tempCol
	}
	return plan.NewCreateTable(planCreate.Database(), planCreate.Name(), newSch, planCreate.IfNotExists(), idxDefs, nil), nil
}
