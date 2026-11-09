package lineage

// Every query leaves out the system schemas the way FEAT-067 does, so lineage
// never draws an object the tree beside it does not show.

const relationsQuery = `
	SELECT c.oid,
	       CASE WHEN c.relispartition THEN pg_catalog.pg_partition_root(c.oid)::oid ELSE 0::oid END,
	       n.nspname::text, c.relname::text, c.relkind::text,
	       COALESCE((SELECT json_agg(a.attname::text ORDER BY a.attnum)
	                 FROM pg_catalog.pg_attribute a
	                 WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped), '[]')::text,
	       CASE WHEN c.relkind IN ('v', 'm') THEN pg_catalog.pg_get_viewdef(c.oid) END
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind IN ('r', 'p', 'v', 'm', 'S')
	  AND n.nspname NOT LIKE 'pg\_%'
	  AND n.nspname <> 'information_schema'`

// Aggregates and window functions are left out, and pg_get_functiondef refuses
// them anyway.
const routinesQuery = `
	SELECT p.oid, n.nspname::text, p.proname::text,
	       pg_catalog.pg_get_function_identity_arguments(p.oid), p.pronargs::int, p.prokind::text,
	       l.lanname::text, pg_catalog.pg_get_functiondef(p.oid), p.prosqlbody IS NOT NULL,
	       EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
	               WHERE d.classid = 'pg_catalog.pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e'),
	       (SELECT substring(setting FROM 'search_path=(.*)')
	        FROM unnest(p.proconfig) setting
	        WHERE setting LIKE 'search\_path=%' LIMIT 1)
	FROM pg_catalog.pg_proc p
	JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
	JOIN pg_catalog.pg_language l ON l.oid = p.prolang
	WHERE p.prokind IN ('f', 'p')
	  AND n.nspname NOT LIKE 'pg\_%'
	  AND n.nspname <> 'information_schema'`

// conparentid <> 0 is a partition's clone of its parent's key, which the fold
// would only draw twice.
const foreignKeysQuery = `
	SELECT c.conrelid, c.confrelid,
	       (SELECT json_agg(a.attname::text ORDER BY k.ord)
	        FROM unnest(c.conkey) WITH ORDINALITY k(num, ord)
	        JOIN pg_catalog.pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.num)::text,
	       (SELECT json_agg(a.attname::text ORDER BY k.ord)
	        FROM unnest(c.confkey) WITH ORDINALITY k(num, ord)
	        JOIN pg_catalog.pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = k.num)::text,
	       c.confdeltype::text, c.confupdtype::text
	FROM pg_catalog.pg_constraint c
	JOIN pg_catalog.pg_class r ON r.oid = c.conrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = r.relnamespace
	WHERE c.contype = 'f'
	  AND c.conparentid = 0
	  AND n.nspname NOT LIKE 'pg\_%'
	  AND n.nspname <> 'information_schema'`

// tgparentid <> 0 is a partition's clone of its parent's trigger.
const triggersQuery = `
	SELECT t.tgname::text, t.tgrelid, t.tgfoid, t.tgtype::int,
	       COALESCE((SELECT json_agg(a.attname::text ORDER BY x.ord)
	                 FROM unnest(t.tgattr::int2[]) WITH ORDINALITY x(num, ord)
	                 JOIN pg_catalog.pg_attribute a ON a.attrelid = t.tgrelid AND a.attnum = x.num), '[]')::text
	FROM pg_catalog.pg_trigger t
	JOIN pg_catalog.pg_class r ON r.oid = t.tgrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = r.relnamespace
	WHERE NOT t.tgisinternal
	  AND t.tgparentid = 0
	  AND n.nspname NOT LIKE 'pg\_%'
	  AND n.nspname <> 'information_schema'`

// Each row: kind, from, from column, whether from is a routine, to relation,
// to column, to routine, a policy's name, and whether a default is a
// generated column's. A missing oid is 0.
const dependenciesQuery = viewReadsQuery + " UNION ALL " + viewCallsQuery + " UNION ALL " +
	policyReadsQuery + " UNION ALL " + defaultsQuery + " UNION ALL " + ownedSequencesQuery +
	" UNION ALL " + atomicBodiesQuery

const viewReadsQuery = `
	SELECT 'view_read', r.ev_class, NULL::text, false, d.refobjid,
	       CASE WHEN d.refobjsubid > 0 THEN a.attname::text END, 0::oid, NULL::text, false
	FROM pg_catalog.pg_rewrite r
	JOIN pg_catalog.pg_class v ON v.oid = r.ev_class
	JOIN pg_catalog.pg_namespace vn ON vn.oid = v.relnamespace
	JOIN pg_catalog.pg_depend d ON d.classid = 'pg_catalog.pg_rewrite'::regclass AND d.objid = r.oid
	 AND d.refclassid = 'pg_catalog.pg_class'::regclass AND d.deptype = 'n'
	LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
	WHERE d.refobjid <> r.ev_class
	  AND v.relkind IN ('v', 'm')
	  AND vn.nspname NOT LIKE 'pg\_%' AND vn.nspname <> 'information_schema'`

const viewCallsQuery = `
	SELECT DISTINCT 'call', r.ev_class, NULL::text, false, 0::oid, NULL::text, d.refobjid, NULL::text, false
	FROM pg_catalog.pg_rewrite r
	JOIN pg_catalog.pg_class v ON v.oid = r.ev_class
	JOIN pg_catalog.pg_namespace vn ON vn.oid = v.relnamespace
	JOIN pg_catalog.pg_depend d ON d.classid = 'pg_catalog.pg_rewrite'::regclass AND d.objid = r.oid
	 AND d.refclassid = 'pg_catalog.pg_proc'::regclass AND d.deptype = 'n'
	JOIN pg_catalog.pg_proc p ON p.oid = d.refobjid
	JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
	WHERE v.relkind IN ('v', 'm')
	  AND vn.nspname NOT LIKE 'pg\_%' AND vn.nspname <> 'information_schema'
	  AND pn.nspname NOT LIKE 'pg\_%' AND pn.nspname <> 'information_schema'`

const policyReadsQuery = `
	SELECT 'policy_read', pol.polrelid, NULL::text, false, d.refobjid,
	       CASE WHEN d.refobjsubid > 0 THEN a.attname::text END, 0::oid, pol.polname::text, false
	FROM pg_catalog.pg_policy pol
	JOIN pg_catalog.pg_class t ON t.oid = pol.polrelid
	JOIN pg_catalog.pg_namespace tn ON tn.oid = t.relnamespace
	JOIN pg_catalog.pg_depend d ON d.classid = 'pg_catalog.pg_policy'::regclass AND d.objid = pol.oid
	 AND d.refclassid = 'pg_catalog.pg_class'::regclass AND d.deptype = 'n'
	LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
	WHERE tn.nspname NOT LIKE 'pg\_%' AND tn.nspname <> 'information_schema'`

// A generated column's reach into its own table is an automatic dependency,
// and a default's reach into a sequence a normal one, so both are read and the
// column's dependency on itself is left out.
const defaultsQuery = `
	SELECT 'default', ad.adrelid, a.attname::text, false,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_class'::regclass THEN d.refobjid ELSE 0::oid END,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_class'::regclass AND d.refobjsubid > 0
	            THEN ra.attname::text END,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_proc'::regclass THEN d.refobjid ELSE 0::oid END,
	       NULL::text, a.attgenerated = 's'
	FROM pg_catalog.pg_attrdef ad
	JOIN pg_catalog.pg_class t ON t.oid = ad.adrelid
	JOIN pg_catalog.pg_namespace tn ON tn.oid = t.relnamespace
	JOIN pg_catalog.pg_attribute a ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
	JOIN pg_catalog.pg_depend d ON d.classid = 'pg_catalog.pg_attrdef'::regclass AND d.objid = ad.oid
	 AND d.refclassid IN ('pg_catalog.pg_class'::regclass, 'pg_catalog.pg_proc'::regclass)
	 AND d.deptype IN ('n', 'a')
	LEFT JOIN pg_catalog.pg_attribute ra ON d.refclassid = 'pg_catalog.pg_class'::regclass
	 AND ra.attrelid = d.refobjid AND ra.attnum = d.refobjsubid
	LEFT JOIN pg_catalog.pg_proc p ON d.refclassid = 'pg_catalog.pg_proc'::regclass AND p.oid = d.refobjid
	LEFT JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
	WHERE NOT (d.refclassid = 'pg_catalog.pg_class'::regclass
	           AND d.refobjid = ad.adrelid AND d.refobjsubid = ad.adnum)
	  AND tn.nspname NOT LIKE 'pg\_%' AND tn.nspname <> 'information_schema'
	  AND (p.oid IS NULL OR (pn.nspname NOT LIKE 'pg\_%' AND pn.nspname <> 'information_schema'))`

const ownedSequencesQuery = `
	SELECT 'default', d.refobjid, a.attname::text, false, d.objid, NULL::text, 0::oid, NULL::text, false
	FROM pg_catalog.pg_depend d
	JOIN pg_catalog.pg_class s ON s.oid = d.objid AND s.relkind = 'S'
	JOIN pg_catalog.pg_namespace sn ON sn.oid = s.relnamespace
	JOIN pg_catalog.pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
	WHERE d.classid = 'pg_catalog.pg_class'::regclass
	  AND d.refclassid = 'pg_catalog.pg_class'::regclass
	  AND d.deptype IN ('a', 'i')
	  AND sn.nspname NOT LIKE 'pg\_%' AND sn.nspname <> 'information_schema'`

// A BEGIN ATOMIC body is the one routine PostgreSQL records dependencies for.
// It records no direction, so every relation row is a routine_read here and
// the domain takes the access from the parse.
const atomicBodiesQuery = `
	SELECT CASE WHEN d.refclassid = 'pg_catalog.pg_proc'::regclass THEN 'call' ELSE 'routine_read' END,
	       p.oid, NULL::text, true,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_class'::regclass THEN d.refobjid ELSE 0::oid END,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_class'::regclass AND d.refobjsubid > 0
	            THEN a.attname::text END,
	       CASE WHEN d.refclassid = 'pg_catalog.pg_proc'::regclass THEN d.refobjid ELSE 0::oid END,
	       NULL::text, false
	FROM pg_catalog.pg_proc p
	JOIN pg_catalog.pg_namespace pns ON pns.oid = p.pronamespace
	JOIN pg_catalog.pg_depend d ON d.classid = 'pg_catalog.pg_proc'::regclass AND d.objid = p.oid
	 AND d.deptype = 'n'
	 AND d.refclassid IN ('pg_catalog.pg_class'::regclass, 'pg_catalog.pg_proc'::regclass)
	LEFT JOIN pg_catalog.pg_attribute a ON d.refclassid = 'pg_catalog.pg_class'::regclass
	 AND a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
	LEFT JOIN pg_catalog.pg_proc rp ON d.refclassid = 'pg_catalog.pg_proc'::regclass AND rp.oid = d.refobjid
	LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid = rp.pronamespace
	WHERE p.prosqlbody IS NOT NULL
	  AND pns.nspname NOT LIKE 'pg\_%' AND pns.nspname <> 'information_schema'
	  AND (rp.oid IS NULL OR (rn.nspname NOT LIKE 'pg\_%' AND rn.nspname <> 'information_schema'))`

const inheritanceQuery = `
	SELECT i.inhrelid, i.inhparent
	FROM pg_catalog.pg_inherits i
	JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE NOT c.relispartition
	  AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'`

// 16384 is FirstNormalObjectId: everything below it came with the server.
const fingerprintQuery = `
	SELECT md5(string_agg(part, ',' ORDER BY part)) FROM (
	  SELECT 'c' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_class WHERE oid >= 16384
	  UNION ALL SELECT 'a' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_attribute WHERE attrelid >= 16384
	  UNION ALL SELECT 'p' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_proc WHERE oid >= 16384
	  UNION ALL SELECT 'k' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_constraint WHERE oid >= 16384
	  UNION ALL SELECT 'r' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_rewrite WHERE oid >= 16384
	  UNION ALL SELECT 't' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_trigger WHERE oid >= 16384
	  UNION ALL SELECT 'y' || count(*) || ':' || COALESCE(sum(xmin::text::bigint), 0) FROM pg_catalog.pg_policy WHERE oid >= 16384
	) s(part)`
