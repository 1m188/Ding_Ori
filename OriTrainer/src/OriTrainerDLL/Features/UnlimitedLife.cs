using System;

namespace OriTrainerDLL.Features
{
    // 无限生命：直接置位游戏自带的"无敌"闸门 SeinDamageReciever.IsImmortal，
    // 从源头拦截一切伤害判定（含 Crush / 溺水 / 尖刺 / 岩浆等致死伤害）。
    public static class UnlimitedLife
    {
        // 置位时 Sein 可能尚未生成（主菜单 / 读档中），返回 false 表示这次没做成；
        // 命令方（Loader）目前对 Start 失败不重试，因此这里做成"幂等 + 静默失败"：
        // 玩家在进游戏后再按一次热键即可补上。
        private static bool TrySet(bool immortal)
        {
            try
            {
                // SeinDamageReciever 是 MonoBehaviour，!= null 走 UnityEngine.Object
                // 的 op_Equality，已销毁的假空同样判 null，不必自己查 m_CachedPtr。
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return false;

                SeinMortality mortality = sein.Mortality;
                if (mortality == null) return false;

                SeinDamageReciever reciever = mortality.DamageReciever;
                if (reciever == null) return false;

                reciever.IsImmortal = immortal;
                return true;
            }
            catch { return false; }
        }

        public static void Start()
        {
            TrySet(true);
        }

        public static void Stop()
        {
            TrySet(false);
        }
    }
}
