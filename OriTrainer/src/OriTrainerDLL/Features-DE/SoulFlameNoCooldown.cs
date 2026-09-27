using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 灵魂链接无需冷却：hook 游戏自己的 HandleCooldown()，让冷却永不衰减，
    // 并在 replacement 里顺带把 m_cooldownRemaining 清 0。
    //
    // ---- 为什么 hook ----
    // hook 后：
    //   · HandleCooldown() 是每帧调用的冷却衰减点，hook 它 = 冷却永不自然衰减；
    //   · 但 CastSoulFlame() 施放时仍会把 m_cooldownRemaining 置 1，若不处理，
    //     HandleCharging() 看到 == 1f 会永远不让蓄力。所以 replacement 里顺手清 0。
    //   · 效果：冷却恒为 0，蓄力判定（m_cooldownRemaining == 0f）恒通过。
    //
    // ---- 为什么 replacement 是"接收 SeinSoulFlame 的实例方法" ----
    // Hook 用 jmp 改写入口，跳转时 this 原样在栈上。HandleCooldown 是私有实例方法
    // （void HandleCooldown()，IL 层带 this），所以 replacement 必须接收 this 参数
    // （SeinSoulFlame），否则栈不平衡。replacement 不调用原方法体（返回即"空操作"）。
    //
    // ---- 停止 ----
    // Stop() 只 Unhook，还原 HandleCooldown 原始字节。之后冷却恢复原逻辑（自然衰减）。
    public static class SoulFlameNoCooldown
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static MethodInfo _target;    // SeinSoulFlame.HandleCooldown
        private static MethodInfo _replacement; // 我们的 replacement
        private static FieldInfo _fCooldown;  // m_cooldownRemaining

        public static void Start()
        {
            if (Hooks.IsHooked(_target)) return; // 幂等：重复 Start 不重复 hook

            Type t = typeof(SeinSoulFlame);
            _target = t.GetMethod("HandleCooldown", Private);
            _fCooldown = t.GetField("m_cooldownRemaining", Private);

            // 字段/方法名对不上就直接失败（Loader 会记进错误日志），而不是静默空转
            if (_target == null || _fCooldown == null)
                throw new Exception("SeinSoulFlame 的 HandleCooldown/m_cooldownRemaining 与预期不符，功能无法工作");

            // replacement 必须是实例方法，签名与 HandleCooldown 兼容（this=SeinSoulFlame）
            _replacement = typeof(SoulFlameNoCooldown).GetMethod("OnHandleCooldown",
                BindingFlags.NonPublic | BindingFlags.Static);

            Hooks.Replace(_target, _replacement);
        }

        public static void Stop()
        {
            if (!Hooks.IsHooked(_target)) return; // 幂等

            Hooks.Unhook(_target);
            _target = null;
            _replacement = null;
            _fCooldown = null;
        }

        // 由游戏主线程每帧调用（代替 HandleCooldown）。
        // 清冷却字段 + 什么都不做（返回即"空操作"）。
        private static void OnHandleCooldown(SeinSoulFlame soulFlame)
        {
            try
            {
                _fCooldown.SetValue(soulFlame, 0f);
            }
            catch { }
        }
    }
}
