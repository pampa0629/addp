package gaussdb

// Kernel 507 uses reloptions/pg_rlspolicy for RLS, parttype for partitions,
// ubtree for native Ustore indexes, and pg_attrdef.adgencol for generated
// columns. These are native facts, not missing PostgreSQL columns defaulted
// to safe values. The protocol reader holds LOCK TABLE ONLY first.
const guardedTableCatalogSQL = `SELECT c.relkind='r' AND c.relpersistence IN ('p','u')
 AND c.parttype='n' AND c.relam=0 AND NOT c.relhasrules
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_rlspolicy policy WHERE policy.polrelid=c.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.unnest(c.reloptions) option(value)
   WHERE CASE WHEN pg_catalog.split_part(option.value,'=',1)='enable_rowsecurity'
     THEN pg_catalog.split_part(option.value,'=',2)::boolean ELSE false END
    OR (pg_catalog.split_part(option.value,'=',1)='orientation' AND pg_catalog.split_part(option.value,'=',2)<>'row'))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhrelid=c.oid OR i.inhparent=c.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_index i
   JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
   JOIN pg_catalog.pg_am am ON am.oid=idx.relam
   WHERE i.indrelid=c.oid AND (i.indexprs IS NOT NULL OR i.indpred IS NOT NULL
    OR am.amname NOT IN ('btree','ubtree')
    OR EXISTS (SELECT 1 FROM pg_catalog.unnest(i.indclass) op
      JOIN pg_catalog.pg_opclass oc ON oc.oid=op
      JOIN pg_catalog.pg_namespace ns ON ns.oid=oc.opcnamespace WHERE ns.nspname<>'pg_catalog')))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
   JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
   JOIN pg_catalog.pg_namespace tn ON tn.oid=t.typnamespace
   WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
    AND (tn.nspname<>'pg_catalog' OR t.typtype<>'b'
     OR EXISTS (SELECT 1 FROM pg_catalog.pg_attrdef d WHERE d.adrelid=a.attrelid AND d.adnum=a.attnum AND d.adgencol<>'')
     OR (t.typelem<>0 AND NOT EXISTS
       (SELECT 1 FROM pg_catalog.pg_type elem JOIN pg_catalog.pg_namespace en ON en.oid=elem.typnamespace
        WHERE elem.oid=t.typelem AND en.nspname='pg_catalog' AND elem.typtype='b'))))
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=$1 AND c.relname=$2`
