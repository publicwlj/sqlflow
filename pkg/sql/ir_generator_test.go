// Copyright 2019 The SQLFlow Authors. All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sql

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	pb "sqlflow.org/sqlflow/pkg/proto"
	"sqlflow.org/sqlflow/pkg/sql/ir"
)

func TestGenerateTrainIR(t *testing.T) {
	a := assert.New(t)
	parser := newExtendedSyntaxParser()

	normal := `
	SELECT c1, c2, c3, c4
	FROM my_table
	TO TRAIN DNNClassifier
	WITH
		model.n_classes=2,
		train.optimizer="adam",
		model.stddev=0.001,
		model.hidden_units=[128,64],
		validation.select="SELECT c1, c2, c3, c4 FROM my_table LIMIT 10"
	COLUMN c1,NUMERIC(c2, [128, 32]),CATEGORY_ID(c3, 512),
		SEQ_CATEGORY_ID(c3, 512),
		CROSS([c1,c2], 64),
		BUCKET(NUMERIC(c1, [100]), 100),
		EMBEDDING(CATEGORY_ID(c3, 512), 128, mean),
		NUMERIC(DENSE(c1, 64, COMMA), [128]),
		CATEGORY_ID(SPARSE(c2, 10000, COMMA), 128),
		SEQ_CATEGORY_ID(SPARSE(c2, 10000, COMMA), 128),
		EMBEDDING(c1, 128, sum),
		EMBEDDING(SPARSE(c2, 10000, COMMA, "int"), 128, sum)
	LABEL c4
	INTO mymodel;
	`

	r, e := parser.Parse(normal)
	a.NoError(e)

	trainIR, err := generateTrainIR(r, "mysql://root:root@tcp(127.0.0.1:3306)/iris?maxAllowedPacket=0")
	a.NoError(err)
	a.Equal("DNNClassifier", trainIR.Estimator)
	a.Equal("SELECT c1, c2, c3, c4\nFROM my_table", trainIR.Select)
	a.Equal("SELECT c1, c2, c3, c4 FROM my_table LIMIT 10", trainIR.ValidationSelect)

	for key, attr := range trainIR.Attributes {
		if key == "model.n_classes" {
			a.Equal(2, attr.(int))
		} else if key == "train.optimizer" {
			a.Equal("adam", attr.(string))
		} else if key == "model.stddev" {
			a.Equal(float32(0.001), attr.(float32))
		} else if key == "model.hidden_units" {
			l, ok := attr.([]interface{})
			a.True(ok)
			a.Equal(128, l[0].(int))
			a.Equal(64, l[1].(int))
		} else if key != "validation.select" {
			a.Failf("error key: %s", key)
		}
	}

	nc, ok := trainIR.Features["feature_columns"][0].(*ir.NumericColumn)
	a.True(ok)
	a.Equal([]int{1}, nc.FieldMeta.Shape)

	nc, ok = trainIR.Features["feature_columns"][1].(*ir.NumericColumn)
	a.True(ok)
	a.Equal("c2", nc.FieldMeta.Name)
	a.Equal([]int{128, 32}, nc.FieldMeta.Shape)

	cc, ok := trainIR.Features["feature_columns"][2].(*ir.CategoryIDColumn)
	a.True(ok)
	a.Equal("c3", cc.FieldMeta.Name)
	a.Equal(int64(512), cc.BucketSize)

	seqcc, ok := trainIR.Features["feature_columns"][3].(*ir.SeqCategoryIDColumn)
	a.True(ok)
	a.Equal("c3", seqcc.FieldMeta.Name)

	cross, ok := trainIR.Features["feature_columns"][4].(*ir.CrossColumn)
	a.True(ok)
	a.Equal("c1", cross.Keys[0].(string))
	a.Equal("c2", cross.Keys[1].(string))
	a.Equal(64, cross.HashBucketSize)

	bucket, ok := trainIR.Features["feature_columns"][5].(*ir.BucketColumn)
	a.True(ok)
	a.Equal(100, bucket.Boundaries[0])
	a.Equal("c1", bucket.SourceColumn.FieldMeta.Name)

	emb, ok := trainIR.Features["feature_columns"][6].(*ir.EmbeddingColumn)
	a.True(ok)
	a.Equal("mean", emb.Combiner)
	a.Equal(128, emb.Dimension)
	embInner, ok := emb.CategoryColumn.(*ir.CategoryIDColumn)
	a.True(ok)
	a.Equal("c3", embInner.FieldMeta.Name)
	a.Equal(int64(512), embInner.BucketSize)

	// NUMERIC(DENSE(c1, [64], COMMA), [128])
	nc, ok = trainIR.Features["feature_columns"][7].(*ir.NumericColumn)
	a.True(ok)
	a.Equal(64, nc.FieldMeta.Shape[0])
	a.Equal(",", nc.FieldMeta.Delimiter)
	a.False(nc.FieldMeta.IsSparse)

	// CATEGORY_ID(SPARSE(c2, 10000, COMMA), 128),
	cc, ok = trainIR.Features["feature_columns"][8].(*ir.CategoryIDColumn)
	a.True(ok)
	a.True(cc.FieldMeta.IsSparse)
	a.Equal("c2", cc.FieldMeta.Name)
	a.Equal(10000, cc.FieldMeta.Shape[0])
	a.Equal(",", cc.FieldMeta.Delimiter)
	a.Equal(int64(128), cc.BucketSize)

	// SEQ_CATEGORY_ID(SPARSE(c2, 10000, COMMA), 128)
	scc, ok := trainIR.Features["feature_columns"][9].(*ir.SeqCategoryIDColumn)
	a.True(ok)
	a.True(scc.FieldMeta.IsSparse)
	a.Equal("c2", scc.FieldMeta.Name)
	a.Equal(10000, scc.FieldMeta.Shape[0])

	// EMBEDDING(c1, 128)
	emb, ok = trainIR.Features["feature_columns"][10].(*ir.EmbeddingColumn)
	a.True(ok)
	a.Equal(nil, emb.CategoryColumn)
	a.Equal(128, emb.Dimension)

	// EMBEDDING(SPARSE(c2, 10000, COMMA, "int"), 128)
	emb, ok = trainIR.Features["feature_columns"][11].(*ir.EmbeddingColumn)
	a.True(ok)
	catCol, ok := emb.CategoryColumn.(*ir.CategoryIDColumn)
	a.True(ok)
	a.True(catCol.FieldMeta.IsSparse)
	a.Equal("c2", catCol.FieldMeta.Name)
	a.Equal(10000, catCol.FieldMeta.Shape[0])
	a.Equal(",", catCol.FieldMeta.Delimiter)

	l, ok := trainIR.Label.(*ir.NumericColumn)
	a.True(ok)
	a.Equal("c4", l.FieldMeta.Name)

	a.Equal("mymodel", trainIR.Into)
}

func TestGenerateTrainIRModelZoo(t *testing.T) {
	a := assert.New(t)
	parser := newExtendedSyntaxParser()

	normal := `
	SELECT c1, c2, c3, c4
	FROM my_table
	TO TRAIN a_data_scientist/regressors:v0.2/MyDNNRegressor
	WITH
		model.n_classes=2,
		train.optimizer="adam"
	LABEL c4
	INTO mymodel;
	`

	r, e := parser.Parse(normal)
	a.NoError(e)

	trainIR, err := generateTrainIR(r, "mysql://root:root@tcp(127.0.0.1:3306)/iris?maxAllowedPacket=0")
	a.NoError(err)
	a.Equal("a_data_scientist/regressors:v0.2", trainIR.ModelImage)
	a.Equal("MyDNNRegressor", trainIR.Estimator)
}
func TestGeneratePredictIR(t *testing.T) {
	if getEnv("SQLFLOW_TEST_DB", "mysql") == "hive" {
		t.Skip(fmt.Sprintf("%s: skip Hive test", getEnv("SQLFLOW_TEST_DB", "mysql")))
	}
	a := assert.New(t)
	parser := newExtendedSyntaxParser()
	predSQL := `SELECT * FROM iris.test
TO PREDICT iris.predict.class
USING sqlflow_models.mymodel;`
	r, e := parser.Parse(predSQL)
	a.NoError(e)

	connStr := "mysql://root:root@tcp(127.0.0.1:3306)/?maxAllowedPacket=0"
	// need to save a model first because predict SQL will read the train SQL
	// from saved model
	modelDir, e := ioutil.TempDir("/tmp", "sqlflow_models")
	a.Nil(e)
	defer os.RemoveAll(modelDir)
	stream := RunSQLProgram(`SELECT * FROM iris.train
TO TRAIN DNNClassifier
WITH model.n_classes=3, model.hidden_units=[10,20]
COLUMN sepal_length, sepal_width, petal_length, petal_width
LABEL class
INTO sqlflow_models.mymodel;`, modelDir, &pb.Session{DbConnStr: connStr})
	a.True(goodStream(stream.ReadAll()))

	predIR, err := generatePredictIR(r, connStr, modelDir, true)
	a.NoError(err)

	a.Equal(connStr, predIR.DataSource)
	a.Equal("iris.predict", predIR.ResultTable)
	a.Equal("class", predIR.TrainIR.Label.GetFieldMeta()[0].Name)
	a.Equal("DNNClassifier", predIR.TrainIR.Estimator)
	nc, ok := predIR.TrainIR.Features["feature_columns"][0].(*ir.NumericColumn)
	a.True(ok)
	a.Equal("sepal_length", nc.FieldMeta.Name)
}

func TestGenerateAnalyzeIR(t *testing.T) {
	if getEnv("SQLFLOW_TEST_DB", "mysql") != "mysql" {
		t.Skip(fmt.Sprintf("%s: skip test", getEnv("SQLFLOW_TEST_DB", "mysql")))
	}
	a := assert.New(t)
	connStr := "mysql://root:root@tcp(127.0.0.1:3306)/?maxAllowedPacket=0"

	modelDir, e := ioutil.TempDir("/tmp", "sqlflow_models")
	a.Nil(e)
	defer os.RemoveAll(modelDir)
	stream := RunSQLProgram(`SELECT * FROM iris.train
TO TRAIN xgboost.gbtree
WITH
	objective="multi:softprob",
	train.num_boost_round = 30,
	eta = 0.4,
	num_class = 3
COLUMN sepal_length, sepal_width, petal_length, petal_width
LABEL class
INTO sqlflow_models.my_xgboost_model;
`, modelDir, &pb.Session{DbConnStr: connStr})
	a.NoError(e)
	a.True(goodStream(stream.ReadAll()))

	pr, e := newExtendedSyntaxParser().Parse(`
	SELECT *
	FROM iris.train
	TO EXPLAIN sqlflow_models.my_xgboost_model
	WITH
	    shap_summary.plot_type="bar",
	    shap_summary.alpha=1,
	    shap_summary.sort=True
	USING TreeExplainer;
	`)
	a.NoError(e)

	AnalyzeIR, e := generateAnalyzeIR(pr, connStr, modelDir, true)
	a.NoError(e)
	a.Equal(AnalyzeIR.DataSource, connStr)
	a.Equal(AnalyzeIR.Explainer, "TreeExplainer")
	a.Equal(len(AnalyzeIR.Attributes), 3)
	a.Equal(AnalyzeIR.Attributes["shap_summary.sort"], true)
	a.Equal(AnalyzeIR.Attributes["shap_summary.plot_type"], "bar")
	a.Equal(AnalyzeIR.Attributes["shap_summary.alpha"], 1)

	nc, ok := AnalyzeIR.TrainIR.Features["feature_columns"][0].(*ir.NumericColumn)
	a.True(ok)
	a.Equal("sepal_length", nc.FieldMeta.Name)
}

func TestInferStringValue(t *testing.T) {
	a := assert.New(t)
	for _, s := range []string{"true", "TRUE", "True"} {
		a.Equal(inferStringValue(s), true)
		a.Equal(inferStringValue(fmt.Sprintf("\"%s\"", s)), s)
		a.Equal(inferStringValue(fmt.Sprintf("'%s'", s)), s)
	}
	for _, s := range []string{"false", "FALSE", "False"} {
		a.Equal(inferStringValue(s), false)
		a.Equal(inferStringValue(fmt.Sprintf("\"%s\"", s)), s)
		a.Equal(inferStringValue(fmt.Sprintf("'%s'", s)), s)
	}
	a.Equal(inferStringValue("t"), "t")
	a.Equal(inferStringValue("F"), "F")
	a.Equal(inferStringValue("1"), 1)
	a.Equal(inferStringValue("\"1\""), "1")
	a.Equal(inferStringValue("'1'"), "1")
	a.Equal(inferStringValue("2.3"), float32(2.3))
	a.Equal(inferStringValue("\"2.3\""), "2.3")
	a.Equal(inferStringValue("'2.3'"), "2.3")
}
