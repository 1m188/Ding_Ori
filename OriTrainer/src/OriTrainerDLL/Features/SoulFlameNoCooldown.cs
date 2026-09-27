using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 灵魂链接无需冷却：hook 游戏自己的 HandleCooldown()，让冷却永不衰减，
    // 并在 replacement 里顺带把 m_cooldownRemaining 清 0。
    //
    // HandleCooldown() 是每帧调用的冷却衰减点，hook 它 = 冷却永不自然衰减；
    // 但 CastSoulFlame() 施放时仍会把 m_cooldownRemaining 置 1，若不处理，
    // HandleCharging() 看到 == 1f 会永远不让蓄力，所以 replacement 里顺手清 0。
    // replacement 接收 this（SeinSoulFlame），与目标签名兼容（jmp 不改栈）。
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