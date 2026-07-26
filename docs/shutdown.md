# Shutdown

`leaseservice.Manager` reserves a bounded handle count and starts renewal only
when the policy enables it. Its `Hooks` method plugs into `service`.

Shutdown order:

1. stop admitting new ownership-dependent work;
2. cancel and join application callbacks;
3. stop managed renewers;
4. explicitly compare-and-release each handle with a bounded context;
5. report every failure.

A canceled shutdown or process exit does not prove remote release. The lease
will remain until successful release or backend expiry.
