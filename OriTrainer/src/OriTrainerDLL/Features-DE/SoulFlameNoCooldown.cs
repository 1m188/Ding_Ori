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
    // Stop() 只还原 HandleCooldown 原始字节。之后冷却恢复原逻辑（自然衰减）。
    public static class SoulFlameNoCooldown
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static Hooks.Hook _hook;
        private static FieldInfo _fCooldown; // m_cooldownRemaining

        public static void Start()
        {
            // 字段/方法名对不上就直接失败（Loader 会记进错误日志），而不是静默空转
            Type t = typeof(SeinSoulFlame);
            MethodInfo target = t.GetMethod("HandleCooldown", Private);
            _fCooldown = t.GetField("m_cooldownRemaining", Private);
            if (target == null || _fCooldown == null)
                throw new Exception("SeinSoulFlame 的 HandleCooldown/m_cooldownRemaining 与预期不符，功能无法工作");

            // HandleCooldown 变空操作；replacement 里把冷却字段清 0。
            // Apply 幂等，重复 Start 安全。
            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(SoulFlameNoCooldown).GetMethod("OnHandleCooldown",
                        BindingFlags.NonPublic | BindingFlags.Static));
        }

        public static void Stop()
        {
            if (_hook == null) return; // 幂等

            _hook.Dispose();
            _hook = null;
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