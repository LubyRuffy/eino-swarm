#include <stdint.h>
typedef struct { uint64_t seconds, microseconds; } zwaiRecoveryBirth;
int zwaiRecoveryCapture(int pid, const char *executable, zwaiRecoveryBirth *birth);
int zwaiRecoveryFocusSuccessor(int pid, const char *executable, zwaiRecoveryBirth birth);
