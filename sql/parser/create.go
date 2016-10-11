// Copyright 2015 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Author: Peter Mattis (peter@cockroachlabs.com)

// This code was derived from https://github.com/youtube/vitess.
//
// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file

package parser

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
)

// CreateDatabase represents a CREATE DATABASE statement.
type CreateDatabase struct {
	IfNotExists bool
	Name        Name
	Encoding    *StrVal
}

// Format implements the NodeFormatter interface.
func (node *CreateDatabase) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("CREATE DATABASE ")
	if node.IfNotExists {
		buf.WriteString("IF NOT EXISTS ")
	}
	FormatNode(buf, f, node.Name)
	if node.Encoding != nil {
		buf.WriteString(" ENCODING=")
		node.Encoding.Format(buf, f)
	}
}

// IndexElem represents a column with a direction in a CREATE INDEX statement.
type IndexElem struct {
	Column    Name
	Direction Direction
}

// Format implements the NodeFormatter interface.
func (node IndexElem) Format(buf *bytes.Buffer, f FmtFlags) {
	FormatNode(buf, f, node.Column)
	if node.Direction != DefaultDirection {
		buf.WriteByte(' ')
		buf.WriteString(node.Direction.String())
	}
}

// IndexElemList is list of IndexElem.
type IndexElemList []IndexElem

// Format pretty-prints the contained names separated by commas.
// Format implements the NodeFormatter interface.
func (l IndexElemList) Format(buf *bytes.Buffer, f FmtFlags) {
	for i, indexElem := range l {
		if i > 0 {
			buf.WriteString(", ")
		}
		FormatNode(buf, f, indexElem)
	}
}

// CreateIndex represents a CREATE INDEX statement.
type CreateIndex struct {
	Name        Name
	Table       NormalizableTableName
	Unique      bool
	IfNotExists bool
	Columns     IndexElemList
	// Extra columns to be stored together with the indexed ones as an optimization
	// for improved reading performance.
	Storing    NameList
	Interleave *InterleaveDef
}

// Format implements the NodeFormatter interface.
func (node *CreateIndex) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("CREATE ")
	if node.Unique {
		buf.WriteString("UNIQUE ")
	}
	buf.WriteString("INDEX ")
	if node.IfNotExists {
		buf.WriteString("IF NOT EXISTS ")
	}
	if node.Name != "" {
		fmt.Fprintf(buf, "%s ", node.Name)
	}
	fmt.Fprintf(buf, "ON %s (", node.Table)
	FormatNode(buf, f, node.Columns)
	buf.WriteByte(')')
	if node.Storing != nil {
		buf.WriteString(" STORING (")
		FormatNode(buf, f, node.Storing)
		buf.WriteByte(')')
	}
	if node.Interleave != nil {
		FormatNode(buf, f, node.Interleave)
	}
}

// TableDef represents a column, index or constraint definition within a CREATE
// TABLE statement.
type TableDef interface {
	NodeFormatter
	// Placeholder function to ensure that only desired types (*TableDef) conform
	// to the TableDef interface.
	tableDef()
	setName(name Name)
}

func (*ColumnTableDef) tableDef() {}
func (*IndexTableDef) tableDef()  {}
func (*FamilyTableDef) tableDef() {}

// TableDefs represents a list of table definitions.
type TableDefs []TableDef

// Format implements the NodeFormatter interface.
func (node TableDefs) Format(buf *bytes.Buffer, f FmtFlags) {
	for i, n := range node {
		if i > 0 {
			buf.WriteString(", ")
		}
		FormatNode(buf, f, n)
	}
}

// Nullability represents either NULL, NOT NULL or an unspecified value (silent
// NULL).
type Nullability int

// The values for NullType.
const (
	NotNull Nullability = iota
	Null
	SilentNull
)

// ColumnTableDef represents a column definition within a CREATE TABLE
// statement.
type ColumnTableDef struct {
	Name     Name
	Type     ColumnType
	Nullable struct {
		Nullability    Nullability
		ConstraintName Name
	}
	PrimaryKey           bool
	Unique               bool
	UniqueConstraintName Name
	DefaultExpr          struct {
		Expr           Expr
		ConstraintName Name
	}
	CheckExprs []ColumnTableDefCheckExpr
	References struct {
		Table          NormalizableTableName
		Col            Name
		ConstraintName Name
	}
	Family struct {
		Name        Name
		Create      bool
		IfNotExists bool
	}
}

// ColumnTableDefCheckExpr represents a check constraint on a column definition
// within a CREATE TABLE statement.
type ColumnTableDefCheckExpr struct {
	Expr           Expr
	ConstraintName Name
}

func newColumnTableDef(
	name Name, typ ColumnType, qualifications []NamedColumnQualification,
) (*ColumnTableDef, error) {
	d := &ColumnTableDef{
		Name: name,
		Type: typ,
	}
	d.Nullable.Nullability = SilentNull
	for _, c := range qualifications {
		switch t := c.Qualification.(type) {
		case *ColumnDefault:
			if d.HasDefaultExpr() {
				return nil, errors.Errorf("multiple default values specified for column %q", name)
			}
			d.DefaultExpr.Expr = t.Expr
			d.DefaultExpr.ConstraintName = c.Name
		case NotNullConstraint:
			if d.Nullable.Nullability == Null {
				return nil, errors.Errorf("conflicting NULL/NOT NULL declarations for column %q", name)
			}
			d.Nullable.Nullability = NotNull
			d.Nullable.ConstraintName = c.Name
		case NullConstraint:
			if d.Nullable.Nullability == NotNull {
				return nil, errors.Errorf("conflicting NULL/NOT NULL declarations for column %q", name)
			}
			d.Nullable.Nullability = Null
			d.Nullable.ConstraintName = c.Name
		case PrimaryKeyConstraint:
			d.PrimaryKey = true
			d.UniqueConstraintName = c.Name
		case UniqueConstraint:
			d.Unique = true
			d.UniqueConstraintName = c.Name
		case *ColumnCheckConstraint:
			d.CheckExprs = append(d.CheckExprs, ColumnTableDefCheckExpr{
				Expr:           t.Expr,
				ConstraintName: c.Name,
			})
		case *ColumnFKConstraint:
			d.References.Table = t.Table
			d.References.Col = t.Col
			d.References.ConstraintName = c.Name
		case *ColumnFamilyConstraint:
			d.Family.Name = t.Family
			d.Family.Create = t.Create
			d.Family.IfNotExists = t.IfNotExists
		default:
			panic(fmt.Sprintf("unexpected column qualification: %T", c))
		}
	}
	return d, nil
}

func (node *ColumnTableDef) setName(name Name) {
	node.Name = name
}

// HasDefaultExpr returns if the ColumnTableDef has a default expression.
func (node *ColumnTableDef) HasDefaultExpr() bool {
	return node.DefaultExpr.Expr != nil
}

// Format implements the NodeFormatter interface.
func (node *ColumnTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	fmt.Fprintf(buf, "%s ", node.Name)
	FormatNode(buf, f, node.Type)
	if node.Nullable.Nullability != SilentNull && node.Nullable.ConstraintName != "" {
		fmt.Fprintf(buf, " CONSTRAINT %s", node.Nullable.ConstraintName)
	}
	switch node.Nullable.Nullability {
	case Null:
		buf.WriteString(" NULL")
	case NotNull:
		buf.WriteString(" NOT NULL")
	}
	if node.PrimaryKey {
		buf.WriteString(" PRIMARY KEY")
	} else if node.Unique {
		buf.WriteString(" UNIQUE")
	}
	if node.HasDefaultExpr() {
		if node.DefaultExpr.ConstraintName != "" {
			fmt.Fprintf(buf, " CONSTRAINT %s", node.DefaultExpr.ConstraintName)
		}
		buf.WriteString(" DEFAULT ")
		FormatNode(buf, f, node.DefaultExpr.Expr)
	}
	for _, checkExpr := range node.CheckExprs {
		if checkExpr.ConstraintName != "" {
			fmt.Fprintf(buf, " CONSTRAINT %s", checkExpr.ConstraintName)
		}
		buf.WriteString(" CHECK (")
		FormatNode(buf, f, checkExpr.Expr)
		buf.WriteByte(')')
	}
	if node.References.Table.TableNameReference != nil {
		if node.References.ConstraintName != "" {
			fmt.Fprintf(buf, " CONSTRAINT %s", node.References.ConstraintName)
		}
		buf.WriteString(" REFERENCES ")
		FormatNode(buf, f, node.References.Table)
		if node.References.Col != "" {
			buf.WriteString(" (")
			FormatNode(buf, f, node.References.Col)
			buf.WriteByte(')')
		}
	}
	if node.Family.Name != "" || node.Family.Create {
		if node.Family.Create {
			buf.WriteString(" CREATE")
		}
		if node.Family.IfNotExists {
			buf.WriteString(" IF NOT EXISTS")
		}
		buf.WriteString(" FAMILY")
		if len(node.Family.Name) > 0 {
			buf.WriteByte(' ')
			FormatNode(buf, f, node.Family.Name)
		}
	}
}

// NamedColumnQualification wraps a NamedColumnQualification with a name.
type NamedColumnQualification struct {
	Name          Name
	Qualification ColumnQualification
}

// ColumnQualification represents a constraint on a column.
type ColumnQualification interface {
	columnQualification()
}

func (*ColumnDefault) columnQualification()          {}
func (NotNullConstraint) columnQualification()       {}
func (NullConstraint) columnQualification()          {}
func (PrimaryKeyConstraint) columnQualification()    {}
func (UniqueConstraint) columnQualification()        {}
func (*ColumnCheckConstraint) columnQualification()  {}
func (*ColumnFKConstraint) columnQualification()     {}
func (*ColumnFamilyConstraint) columnQualification() {}

// ColumnDefault represents a DEFAULT clause for a column.
type ColumnDefault struct {
	Expr Expr
}

// NotNullConstraint represents NOT NULL on a column.
type NotNullConstraint struct{}

// NullConstraint represents NULL on a column.
type NullConstraint struct{}

// PrimaryKeyConstraint represents NULL on a column.
type PrimaryKeyConstraint struct{}

// UniqueConstraint represents UNIQUE on a column.
type UniqueConstraint struct{}

// ColumnCheckConstraint represents either a check on a column.
type ColumnCheckConstraint struct {
	Expr Expr
}

// ColumnFKConstraint represents a FK-constaint on a column.
type ColumnFKConstraint struct {
	Table NormalizableTableName
	Col   Name // empty-string means use PK
}

// ColumnFamilyConstraint represents FAMILY on a column.
type ColumnFamilyConstraint struct {
	Family      Name
	Create      bool
	IfNotExists bool
}

// NameListToIndexElems converts a NameList to an IndexElemList with all
// members using the `DefaultDirection`.
func NameListToIndexElems(lst NameList) IndexElemList {
	elems := make(IndexElemList, 0, len(lst))
	for _, n := range lst {
		elems = append(elems, IndexElem{Column: n, Direction: DefaultDirection})
	}
	return elems
}

// IndexTableDef represents an index definition within a CREATE TABLE
// statement.
type IndexTableDef struct {
	Name       Name
	Columns    IndexElemList
	Storing    NameList
	Interleave *InterleaveDef
}

func (node *IndexTableDef) setName(name Name) {
	node.Name = name
}

// Format implements the NodeFormatter interface.
func (node *IndexTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("INDEX ")
	if node.Name != "" {
		FormatNode(buf, f, node.Name)
		buf.WriteByte(' ')
	}
	buf.WriteByte('(')
	FormatNode(buf, f, node.Columns)
	buf.WriteByte(')')
	if node.Storing != nil {
		buf.WriteString(" STORING (")
		FormatNode(buf, f, node.Storing)
		buf.WriteByte(')')
	}
	if node.Interleave != nil {
		FormatNode(buf, f, node.Interleave)
	}
}

// ConstraintTableDef represents a constraint definition within a CREATE TABLE
// statement.
type ConstraintTableDef interface {
	TableDef
	// Placeholder function to ensure that only desired types
	// (*ConstraintTableDef) conform to the ConstraintTableDef interface.
	constraintTableDef()
}

func (*UniqueConstraintTableDef) constraintTableDef() {}

// UniqueConstraintTableDef represents a unique constraint within a CREATE
// TABLE statement.
type UniqueConstraintTableDef struct {
	IndexTableDef
	PrimaryKey bool
}

// Format implements the NodeFormatter interface.
func (node *UniqueConstraintTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	if node.Name != "" {
		fmt.Fprintf(buf, "CONSTRAINT %s ", node.Name)
	}
	if node.PrimaryKey {
		buf.WriteString("PRIMARY KEY ")
	} else {
		buf.WriteString("UNIQUE ")
	}
	buf.WriteByte('(')
	FormatNode(buf, f, node.Columns)
	buf.WriteByte(')')
	if node.Storing != nil {
		buf.WriteString(" STORING (")
		FormatNode(buf, f, node.Storing)
		buf.WriteByte(')')
	}
	if node.Interleave != nil {
		FormatNode(buf, f, node.Interleave)
	}
}

// ForeignKeyConstraintTableDef represents a FOREIGN KEY constraint in the AST.
type ForeignKeyConstraintTableDef struct {
	Name     Name
	Table    NormalizableTableName
	FromCols NameList
	ToCols   NameList
}

// Format implements the NodeFormatter interface.
func (node *ForeignKeyConstraintTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	if node.Name != "" {
		fmt.Fprintf(buf, "CONSTRAINT %s ", node.Name)
	}
	buf.WriteString("FOREIGN KEY (")
	FormatNode(buf, f, node.FromCols)
	buf.WriteString(") REFERENCES ")
	FormatNode(buf, f, node.Table)

	if len(node.ToCols) > 0 {
		buf.WriteByte(' ')
		buf.WriteByte('(')
		FormatNode(buf, f, node.ToCols)
		buf.WriteByte(')')
	}
}

func (node *ForeignKeyConstraintTableDef) setName(name Name) {
	node.Name = name
}

func (*ForeignKeyConstraintTableDef) tableDef()           {}
func (*ForeignKeyConstraintTableDef) constraintTableDef() {}

func (*CheckConstraintTableDef) tableDef()           {}
func (*CheckConstraintTableDef) constraintTableDef() {}

// CheckConstraintTableDef represents a check constraint within a CREATE
// TABLE statement.
type CheckConstraintTableDef struct {
	Name Name
	Expr Expr
}

func (node *CheckConstraintTableDef) setName(name Name) {
	node.Name = name
}

// Format implements the NodeFormatter interface.
func (node *CheckConstraintTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	if node.Name != "" {
		fmt.Fprintf(buf, "CONSTRAINT %s ", node.Name)
	}
	fmt.Fprintf(buf, "CHECK (")
	FormatNode(buf, f, node.Expr)
	buf.WriteByte(')')
}

// FamilyTableDef represents a family definition within a CREATE TABLE
// statement.
type FamilyTableDef struct {
	Name    Name
	Columns NameList
}

func (node *FamilyTableDef) setName(name Name) {
	node.Name = name
}

// Format implements the NodeFormatter interface.
func (node *FamilyTableDef) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("FAMILY ")
	if node.Name != "" {
		FormatNode(buf, f, node.Name)
		buf.WriteByte(' ')
	}
	buf.WriteByte('(')
	FormatNode(buf, f, node.Columns)
	buf.WriteByte(')')
}

// InterleaveDef represents an interleave definition within a CREATE TABLE
// or CREATE INDEX statement.
type InterleaveDef struct {
	Parent       NormalizableTableName
	Fields       NameList
	DropBehavior DropBehavior
}

// Format implements the NodeFormatter interface.
func (node *InterleaveDef) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString(" INTERLEAVE IN PARENT ")
	FormatNode(buf, f, node.Parent)
	buf.WriteString(" (")
	for i, field := range node.Fields {
		if i > 0 {
			buf.WriteString(", ")
		}
		FormatNode(buf, f, field)
	}
	buf.WriteString(")")
	if node.DropBehavior != DropDefault {
		buf.WriteString(" ")
		buf.WriteString(node.DropBehavior.String())
	}
}

// CreateTable represents a CREATE TABLE statement.
type CreateTable struct {
	IfNotExists   bool
	Table         NormalizableTableName
	Interleave    *InterleaveDef
	Defs          TableDefs
	AsSource      *Select
	AsColumnNames NameList // Only to be used in conjunction with AsSource
}

// As returns true if this table represents a CREATE TABLE ... AS statement,
// false otherwise.
func (node *CreateTable) As() bool {
	return node.AsSource != nil
}

// Format implements the NodeFormatter interface.
func (node *CreateTable) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("CREATE TABLE ")
	if node.IfNotExists {
		buf.WriteString("IF NOT EXISTS ")
	}
	FormatNode(buf, f, node.Table)
	if node.As() {
		if len(node.AsColumnNames) > 0 {
			buf.WriteString(" (")
			FormatNode(buf, f, node.AsColumnNames)
			buf.WriteByte(')')
		}
		buf.WriteString(" AS ")
		FormatNode(buf, f, node.AsSource)
	} else {
		buf.WriteString(" (")
		FormatNode(buf, f, node.Defs)
		buf.WriteByte(')')
		if node.Interleave != nil {
			FormatNode(buf, f, node.Interleave)
		}
	}
}

// CreateView represents a CREATE VIEW statement.
type CreateView struct {
	Name        NormalizableTableName
	ColumnNames NameList
	AsSource    *Select
}

// Format implements the NodeFormatter interface.
func (node *CreateView) Format(buf *bytes.Buffer, f FmtFlags) {
	buf.WriteString("CREATE VIEW ")
	FormatNode(buf, f, node.Name)

	if len(node.ColumnNames) > 0 {
		buf.WriteByte(' ')
		buf.WriteByte('(')
		FormatNode(buf, f, node.ColumnNames)
		buf.WriteByte(')')
	}

	buf.WriteString(" AS ")
	FormatNode(buf, f, node.AsSource)
}
