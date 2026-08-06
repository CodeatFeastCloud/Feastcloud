import { createUuidV7, getDeviceId } from './kitchen';
import { coreApiBase } from './coreInventory';

export type ProductionStatus = 'planned' | 'in_progress' | 'completed' | 'cancelled';
export interface ProductionBatch {
  id: string; outletId: string; stationId?: string; recipeVersionId: string; recipeName: string;
  outputIngredientId: string; outputIngredient: string; outputUnitId: string; outputUnitSymbol: string;
  status: ProductionStatus; plannedQuantity: number; actualQuantity?: number; plannedFor: string;
  startedAt?: string; completedAt?: string; expiresAt?: string; lotCode?: string; notes?: string; version: number;
}
export interface ProductionStation { id:string; name:string; type:string }

function auth(tenantId: string): {actorId:string;headers:Record<string,string>} {
  const token=sessionStorage.getItem('feastcloud.oidc-access-token');
  if(token)return{actorId:'manager-dashboard',headers:{Authorization:`Bearer ${token}`}};
  return{actorId:'manager-dashboard',headers:{'X-FeastCloud-Tenant-ID':tenantId,'X-FeastCloud-Actor-ID':'manager-dashboard'}};
}

async function mutation(apiBase:string,path:string,tenantId:string,outletId:string,payload:Record<string,unknown>):Promise<ProductionBatch>{
  const operationId=createUuidV7();const context=auth(tenantId);const envelope={id:operationId,tenantId,outletId,deviceId:getDeviceId(),actorId:context.actorId,occurredAt:new Date().toISOString(),source:'feastcloud.web',sourceId:operationId,schemaVersion:'1.0',idempotencyKey:operationId,payload};
  const response=await fetch(`${apiBase}${path}`,{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':operationId,...context.headers},body:JSON.stringify(envelope)});
  if(!response.ok)throw new Error(`Production service returned ${response.status}`);
  const body=await response.json() as {data?:ProductionBatch};if(!body.data)throw new Error('Production response is invalid');return body.data;
}

export async function fetchProductionBatches(apiBase:string,tenantId:string,outletId:string):Promise<ProductionBatch[]>{
  const response=await fetch(`${apiBase}/production-batches?outletId=${encodeURIComponent(outletId)}`,{headers:{Accept:'application/json',...auth(tenantId).headers},cache:'no-store'});
  if(!response.ok)throw new Error(`Production service returned ${response.status}`);const body=await response.json() as {data?:unknown};if(!Array.isArray(body.data))throw new Error('Production response is invalid');return body.data as ProductionBatch[];
}

export async function fetchProductionStations(apiBase:string,tenantId:string,outletId:string):Promise<ProductionStation[]>{
  const response=await fetch(`${apiBase}/stations?outletId=${encodeURIComponent(outletId)}`,{headers:{Accept:'application/json',...auth(tenantId).headers},cache:'no-store'});
  if(!response.ok)throw new Error(`Station service returned ${response.status}`);const body=await response.json() as {data?:unknown};if(!Array.isArray(body.data))throw new Error('Station response is invalid');return body.data as ProductionStation[];
}

export function createProductionBatch(apiBase:string,tenantId:string,outletId:string,input:{stationId?:string;recipeVersionId:string;outputIngredientId:string;outputUnitId:string;plannedQuantity:number;plannedFor:string;lotCode:string;notes:string}){
  return mutation(apiBase,'/production-batches',tenantId,outletId,{id:createUuidV7(),outletId,...input});
}

export function transitionProductionBatch(apiBase:string,tenantId:string,outletId:string,batch:ProductionBatch,toStatus:ProductionStatus,input?:{actualQuantity?:number;expiresAt?:string;lotCode?:string;notes?:string}){
  return mutation(apiBase,`/production-batches/${batch.id}/transitions`,tenantId,outletId,{toStatus,expectedVersion:batch.version,...input});
}

export const productionApiBase=coreApiBase;
