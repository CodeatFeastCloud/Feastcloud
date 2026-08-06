import { describe, expect, it } from 'vitest';
import { parseOrderCSV } from './corePlanning';

describe('CSV order intake',()=>{
  it('parses the documented columns and quoted values',()=>{
    const rows=parseOrderCSV('externalRef,placedAt,orderType,itemCode,quantity,note\n"web,42",2026-08-03T12:00:00Z,takeaway,RICE-BOWL,2,"no onion, please"');
    expect(rows).toHaveLength(1);expect(rows[0]).toMatchObject({rowNumber:2,externalRef:'web,42',orderType:'takeaway',itemCode:'RICE-BOWL',quantity:2,rawData:{note:'no onion, please'}});
  });
  it('rejects files that omit a canonical column',()=>{
    expect(()=>parseOrderCSV('externalRef,placedAt,itemCode,quantity\n1,2026-08-03T12:00:00Z,RICE,1')).toThrow('Missing column: orderType');
  });
});
