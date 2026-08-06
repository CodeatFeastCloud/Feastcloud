import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchProductionBatches, transitionProductionBatch, type ProductionBatch } from './coreProduction';

const tenant='11111111-1111-4111-8111-111111111111';
const outlet='33333333-3333-4333-8333-333333333333';
const batch:ProductionBatch={id:'77777777-7777-4777-8777-777777777777',outletId:outlet,recipeVersionId:'88888888-8888-4888-8888-888888888888',recipeName:'Mother sauce',outputIngredientId:'99999999-9999-4999-8999-999999999999',outputIngredient:'Prepared sauce',outputUnitId:'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',outputUnitSymbol:'kg',status:'in_progress',plannedQuantity:10,plannedFor:'2026-08-03T12:00:00Z',version:4};

afterEach(()=>vi.unstubAllGlobals());

describe('core production client',()=>{
  it('reads the outlet-scoped preparation queue',async()=>{
    const fetchMock=vi.fn().mockResolvedValue(new Response(JSON.stringify({data:[batch]}),{status:200,headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetchMock);
    await expect(fetchProductionBatches('http://core/api/v1',tenant,outlet)).resolves.toEqual([batch]);
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining(`outletId=${outlet}`),expect.objectContaining({cache:'no-store'}));
  });

  it('sends the expected aggregate version and actual yield on completion',async()=>{
    const fetchMock=vi.fn().mockResolvedValue(new Response(JSON.stringify({data:{...batch,status:'completed',version:5}}),{status:200,headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetchMock);
    await transitionProductionBatch('http://core/api/v1',tenant,outlet,batch,'completed',{actualQuantity:8.5,lotCode:'LOT-7'});
    const request=fetchMock.mock.calls[0][1] as RequestInit;const envelope=JSON.parse(String(request.body)) as {payload:Record<string,unknown>};
    expect(envelope.payload).toMatchObject({toStatus:'completed',expectedVersion:4,actualQuantity:8.5,lotCode:'LOT-7'});
    expect(request.headers).toMatchObject({'Idempotency-Key':expect.any(String)});
  });
});
