import { ref } from 'vue';
import { api, ApiError } from '../api/client';
import type { components } from '../api/generated/admin';
import { t } from '../i18n';
export type UnifiedModel = components['schemas']['UnifiedModel'];
export type UnifiedDetail = components['schemas']['UnifiedDetail'];
export type ModelPolicy = components['schemas']['ModelPolicy'];
export type Group = components['schemas']['Group'];
export type Grant = components['schemas']['UnifiedGrant'];
export const compatibilityAliases = ref<Record<string,string>>({});
export const unifiedModels = ref<UnifiedModel[]>([]);
export const routingRevision = ref(0);
let loading: Promise<void> | undefined;
// A display snapshot shared by the two editors, never used to authorize calls.
export function reloadUnifiedModels(): Promise<void> {
    if (loading)
        return loading;
    loading = (async () => {
        const models: UnifiedModel[] = [];
        let after = '', revision = 0;
        let aliases: Record<string,string> = {};
        do {
            const page = await api<components['schemas']['UnifiedList']>('/admin/api/unified-models?limit=200' + (after ? '&after=' + encodeURIComponent(after) : ''));
            if (!Array.isArray(page.models) || !page.routing_revision)
                throw new Error(t('统一模型列表格式无效'));
            if (revision && revision !== page.routing_revision)
                throw new ApiError(t('分页期间模型配置已变化，请重新读取'), 409, 'revision_conflict');
            revision = page.routing_revision;
            aliases = page.compatibility_aliases ?? {};
            models.push(...page.models);
            if (models.length > 16384 || (page.next_after && page.next_after <= after))
                throw new Error(t('统一模型分页结果无效'));
            after = page.next_after;
        } while (after);
        compatibilityAliases.value = aliases;
        unifiedModels.value = models;
        routingRevision.value = revision;
    })().finally(() => { loading = undefined; });
    return loading;
}
export const canonicalSource = (id: string): string => id.includes('/') ? id : 'codearts/' + id;
export const rawSource = (id: string): string => id.replace(/^codearts\//, '');
export function editError(error: unknown): string {
    const message = error instanceof Error ? error.message : String(error);
    return error instanceof ApiError && (error.status === 428 || ['revision_conflict', 'model_configuration_changed'].includes(error.reason ?? '') || (error.status === 409 && !error.reason))
        ? message + ' · ' + t('草稿已保留，请重新读取后核对；不会自动覆盖新配置。') : message;
}
export interface MigrationIssue {
    reason: string;
    group_ids: string[];
    model_name: string;
    field_path: string;
    message: string;
}
export interface MigrationReport {
    from_schema: number;
    to_schema: number;
    ready: boolean;
    definitions: number;
    group_changes: number;
    conflicts: MigrationIssue[];
    warnings: MigrationIssue[];
}
export function parseMigrationReport(text: string): MigrationReport {
    if (new TextEncoder().encode(text).length > 1 << 20)
        throw new Error(t('迁移报告不能超过 1 MiB'));
    const data = JSON.parse(text) as MigrationReport;
    if (!data || !Number.isInteger(data.from_schema) || data.to_schema !== 11 || typeof data.ready !== 'boolean' || !Number.isInteger(data.definitions) || !Number.isInteger(data.group_changes))
        throw new Error(t('请选择迁移预检查生成的报告，不要上传状态或凭据文件。'));
    for (const rows of [data.conflicts, data.warnings]) {
        if (!Array.isArray(rows) || rows.length > 16384 || rows.some(row => !row || !['reason', 'model_name', 'field_path', 'message'].every(key => typeof row[key as keyof MigrationIssue] === 'string') || !Array.isArray(row.group_ids) || row.group_ids.some(id => typeof id !== 'string')))
            throw new Error(t('迁移报告格式无效'));
    }
    return data;
}
