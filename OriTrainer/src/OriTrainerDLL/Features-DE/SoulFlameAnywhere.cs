using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 可在不安全区域 / 不平稳地面建立灵魂链接。
    //
    // ---- 原理：hook 安全判定本身，而不是绕过它 ----
    // 游戏把所有"能否在此处建立灵魂链接"的判定汇聚在属性 getter
    // SeinSoulFlame.IsSafeToCastSoulFlame（返回 SoulFlamePlacementSafety 枚举）。
    // hook 版把 getter 换成恒返回 Safe，让游戏自己走正常逻辑：
    // HandleCharging() 看到 Safe → 正常涨蓄力到 1 → CastSoulFlame() 施放。
    //
    // 因此不需要旧实现的持续抑制回退（HoldDownDuration=+Inf）+ 清落地延迟 +
    // 写满蓄力（绕过判定填结果），也不需要防连发（蓄力由游戏自然涨，到 1 才放）。
    //
    // ---- 已知副作用 ----
    // AllowedToAccessSkillTree 也读 IsSafeToCastSoulFlame（== Safe 才可开技能树），
    // 恒 Safe 后技能树在任意位置都能打开（升级过即可）。
    //
    // ---- 停止 ----
    // Stop() 只还原 getter 原始字节，安全判定恢复真实结果。
    public static class SoulFlameAnywhere
    {
        private static Hooks.Hook _hook;

        public static void Start()
        {
            // C# 属性 getter 在 IL 里是 specialname 方法 get_IsSafeToCastSoulFlame。
            // 用 GetProperty 拿属性再 GetGetMethod(true) 拿 getter（避免 specialname
            // 过滤导致 GetMethod("get_...") 找 null）。它是 public 属性的 getter。
            PropertyInfo prop = typeof(SeinSoulFlame).GetProperty("IsSafeToCastSoulFlame",
                BindingFlags.Public | BindingFlags.Instance);
            // 属性名对不上就直接失败（Loader 会记进错误日志），而不是静默空转。
            MethodInfo target = (prop?.GetGetMethod(true)) ?? throw new Exception("SeinSoulFlame.IsSafeToCastSoulFlame 与预期不符，功能无法工作");

            // 恒返回 Safe：告诉游戏任何位置都能建链接。Apply 幂等，重复 Start 安全。

            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(SoulFlameAnywhere).GetMethod("OnIsSafeToCastSoulFlame",
                        BindingFlags.NonPublic | BindingFlags.Static));
        }

        public static void Stop()
        {
            if (_hook == null) return; // 幂等

            _hook.Dispose();
            _hook = null;
        }

        // 由游戏在需要判断"能否建立灵魂链接"时调用（代替 get_IsSafeToCastSoulFlame）。
        // 恒返回 Safe：告诉游戏任何位置都能建链接。
        private static SeinSoulFlame.SoulFlamePlacementSafety OnIsSafeToCastSoulFlame(SeinSoulFlame soulFlame)
        {
            return SeinSoulFlame.SoulFlamePlacementSafety.Safe;
        }
    }
}