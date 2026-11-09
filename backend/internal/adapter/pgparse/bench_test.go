package pgparse

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	pgquery "github.com/wasilibs/go-pgquery"
)

// scaleRoutines is the study's scale fixture's routine mix, as
// pg_get_functiondef writes it: 250 PL/pgSQL functions of one to eight
// five-statement blocks, 20 that build SQL at run time, and 30 BEGIN ATOMIC
// bodies. One more, from the seed, makes 301.
func scaleRoutines() [][2]string {
	r := rand.New(rand.NewPCG(42, 42))
	table := func() string { n := 1 + r.IntN(2000); return fmt.Sprintf("s%d.t%d", n%10, n) }
	var out [][2]string
	for i := 1; i <= 250; i++ {
		var body strings.Builder
		for k := 1; k <= 1+i%8; k++ {
			fmt.Fprintf(&body, `
  SELECT sum(c3) INTO v FROM %s WHERE c1 = p AND c6 > %d;
  UPDATE %s SET c4 = v, c5 = 'x' WHERE id = p;
  INSERT INTO %s (id, c1, c2, c3) SELECT id, c1, c2, c3 FROM %s WHERE c6 > 0 AND ref_id = p;
  WITH gone AS (DELETE FROM %s WHERE c1 = p AND c7 IS NULL RETURNING id) SELECT count(*) INTO n FROM gone;
  IF n > 0 THEN
    UPDATE %s SET c6 = c6 + n WHERE id = p;
  END IF;`, table(), k, table(), table(), table(), table(), table())
		}
		if i > 1 {
			fmt.Fprintf(&body, "\n  PERFORM s%d.fn%d(p);", (i-1)%10, i-1)
		}
		out = append(out, [2]string{fmt.Sprintf(`CREATE OR REPLACE FUNCTION s%d.fn%d(p bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE v numeric; n bigint;
BEGIN%s
END $function$
`, i%10, i, body.String()), "plpgsql"})
	}
	for i := 1; i <= 20; i++ {
		out = append(out, [2]string{fmt.Sprintf(`CREATE OR REPLACE FUNCTION s%d.dyn%d(tbl text, p bigint)
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE n bigint;
BEGIN
  EXECUTE format('UPDATE %%I SET c6 = 0 WHERE id = $1', tbl) USING p;
  EXECUTE 'SELECT count(*) FROM s0.t10' INTO n;
  RETURN n;
END $function$
`, i%10, i), "plpgsql"})
	}
	for i := 1; i <= 30; i++ {
		out = append(out, [2]string{fmt.Sprintf(`CREATE OR REPLACE FUNCTION s%d.atomic%d(p bigint)
 RETURNS numeric
 LANGUAGE sql
 STABLE
BEGIN ATOMIC
 SELECT sum(a.c3) AS sum
    FROM (%s a
      JOIN %s b ON ((b.id = a.ref_id)))
   WHERE (a.c1 = atomic%d.p);
END
`, i%10, i, table(), table(), i), "sql"})
	}
	return append(out, [2]string{fromForms, "plpgsql"})
}

// BenchmarkParse301Routines is ADR 0009 P24's parse half: every routine of the
// scale fixture, once. The module's one-off compile is reported separately.
func BenchmarkParse301Routines(b *testing.B) {
	compileOnce.Do(func() {
		start := time.Now()
		if _, err := pgquery.Parse("SELECT 1"); err != nil {
			b.Fatal(err)
		}
		compile = time.Since(start)
	})
	routines := scaleRoutines()
	bytes := 0
	for _, r := range routines {
		bytes += len(r[0])
	}
	b.ResetTimer()
	for range b.N {
		for _, r := range routines {
			body, err := ParseRoutine(r[0], r[1])
			if err != nil || body.Status != "parsed" {
				b.Fatalf("%s: %v", body.Message, err)
			}
		}
	}
	b.ReportMetric(float64(compile.Milliseconds()), "compile-ms")
	b.ReportMetric(float64(bytes/len(routines)), "bytes/routine")
}

var (
	compileOnce sync.Once
	compile     time.Duration
)
