<template>
  <el-dialog
    :model-value="modelValue"
    :title="`设置凭证自动轮换 - ${alias?.address || ''}`"
    width="min(560px, calc(100vw - 28px))"
    :close-on-click-modal="false"
    :show-close="!saving"
    :close-on-press-escape="!saving"
    @update:model-value="updateVisibility"
  >
    <div v-if="alias" class="auto-rotate-dialog-content">
      <el-alert
        v-if="alias.autoRotateDurationMinutes > 0 && alias.autoRotateDeadline"
        type="warning"
        show-icon
        :closable="false"
        class="auto-rotate-status-banner"
      >
        <template #title>
          <strong>倒计时进行中</strong>
        </template>
        <div>
          首条验证码已触发，预计将于
          <strong>{{ formatTime(alias.autoRotateDeadline, { seconds: true }) }}</strong>
          自动轮换整套凭证（设定窗口 {{ alias.autoRotateDurationMinutes }} 分钟）。
        </div>
      </el-alert>

      <el-alert
        v-else-if="alias.autoRotateDurationMinutes > 0"
        type="info"
        show-icon
        :closable="false"
        class="auto-rotate-status-banner"
      >
        <template #title>
          <strong>待命中（等待首条验证码）</strong>
        </template>
        <div>
          已设定有效时长为 <strong>{{ alias.autoRotateDurationMinutes }} 分钟</strong>。当前未收到新验证码，凭据继续正常可用；收到首条验证码后将正式启动倒计时。
        </div>
      </el-alert>

      <el-alert
        v-else
        type="info"
        :closable="false"
        class="auto-rotate-status-banner"
        title="未启用自动轮换"
        description="该邮箱凭证不会自动轮换，如需更换需手动在操作栏点击轮换。"
      />

      <el-form label-position="top" class="auto-rotate-form" :disabled="saving">
        <el-form-item label="轮换整套凭证有效时长">
          <el-radio-group v-model="selectedOption" class="auto-rotate-options">
            <el-radio-button :value="0">关闭</el-radio-button>
            <el-radio-button :value="30">30 分钟</el-radio-button>
            <el-radio-button :value="60">60 分钟 (1小时)</el-radio-button>
            <el-radio-button :value="120">120 分钟 (2小时)</el-radio-button>
            <el-radio-button value="custom">自定义</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item
          v-if="selectedOption === 'custom'"
          label="自定义分钟数（1 ~ 10080 分钟）"
        >
          <el-input-number
            v-model="customMinutes"
            :min="1"
            :max="10080"
            :step="10"
            style="width: 200px"
          />
          <span class="custom-minutes-unit">分钟</span>
        </el-form-item>
      </el-form>

      <div class="auto-rotate-rules-card">
        <strong>💡 规则机制说明：</strong>
        <ol>
          <li><strong>无码待命</strong>：保存设置后进入待命状态。若未收到新验证码，凭证长期有效，<strong>绝不会提前轮换</strong>。</li>
          <li><strong>接码起算</strong>：收到首条含 6 位验证码邮件时，以此时间点<strong>正式启动倒计时</strong>。</li>
          <li><strong>固定生命周期</strong>：倒计时期间再次收到验证码<strong>不顺延</strong>，到期后准时轮换。</li>
          <li><strong>自动全套失效</strong>：倒计时结束时后台自动执行整套凭据轮换（API Key、IMAP 密码、访问令牌立即全部失效）。</li>
          <li><strong>单次生效</strong>：轮换完成后该规则自动关闭（恢复为不自动轮换）。</li>
        </ol>
      </div>
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button :disabled="saving" @click="updateVisibility(false)">取消</el-button>
        <el-button
          v-if="alias?.autoRotateDurationMinutes > 0"
          type="danger"
          plain
          :loading="saving"
          @click="submitDisable"
        >
          关闭自动轮换
        </el-button>
        <el-button type="primary" :loading="saving" @click="submitSave">
          保存设置
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { setAliasAutoRotate } from "../api/admin.js";
import { formatTime } from "../utils/format.js";

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false,
  },
  alias: {
    type: Object,
    default: null,
  },
  csrfToken: {
    type: String,
    default: "",
  },
});

const emit = defineEmits(["update:modelValue", "updated"]);

const selectedOption = ref(0);
const customMinutes = ref(30);
const saving = ref(false);
let dialogSession = 0;

function updateVisibility(value) {
  if (!saving.value) emit("update:modelValue", value);
}

onBeforeUnmount(() => { ++dialogSession; });

watch(
  [() => props.modelValue, () => props.alias],
  ([visible, item]) => {
    ++dialogSession;
    saving.value = false;
    if (!visible) return;
    customMinutes.value = 30;
    if (!item) {
      selectedOption.value = 0;
      customMinutes.value = 30;
      return;
    }
    const mins = item.autoRotateDurationMinutes || 0;
    if (mins === 0 || mins === 30 || mins === 60 || mins === 120) {
      selectedOption.value = mins;
    } else {
      selectedOption.value = "custom";
      customMinutes.value = mins;
    }
  },
  { immediate: true, flush: "sync" },
);

async function submitDuration(targetMinutes, failureMessage) {
  if (!props.alias || !props.modelValue || saving.value) return;
  const session = dialogSession;
  const aliasID = props.alias.id;
  saving.value = true;
  try {
    const result = await setAliasAutoRotate(aliasID, targetMinutes, props.csrfToken);
    if (session !== dialogSession) return;
    ElMessage.success(targetMinutes > 0
      ? `已设置接码后 ${targetMinutes} 分钟自动轮换整套凭证`
      : "已关闭该邮箱的凭据自动轮换");
    emit("updated", result.alias);
    emit("update:modelValue", false);
  } catch (err) {
    if (session === dialogSession) ElMessage.error(err?.message || failureMessage);
  } finally {
    if (session === dialogSession) saving.value = false;
  }
}

async function submitDisable() {
  return submitDuration(0, "关闭自动轮换失败，请稍后重试");
}

async function submitSave() {
  if (!props.alias || !props.modelValue || saving.value) return;
  const custom = selectedOption.value === "custom";
  const targetMinutes = Number(custom ? customMinutes.value : selectedOption.value);
  if (!Number.isInteger(targetMinutes) || targetMinutes < (custom ? 1 : 0) || targetMinutes > 10080) {
    ElMessage.error("自定义轮换时间必须是 1 到 10080 之间的整数分钟。");
    return;
  }
  return submitDuration(targetMinutes, "保存设置失败，请稍后重试");
}
</script>

<style scoped>
.auto-rotate-dialog-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.auto-rotate-status-banner {
  margin-bottom: 4px;
}
.auto-rotate-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.custom-minutes-unit {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
}
.auto-rotate-rules-card {
  padding: 12px 14px;
  background-color: var(--el-fill-color-light);
  border-radius: 6px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
}
.auto-rotate-rules-card ol {
  margin: 6px 0 0 18px;
  padding: 0;
}
.auto-rotate-rules-card li {
  margin-bottom: 4px;
}
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
